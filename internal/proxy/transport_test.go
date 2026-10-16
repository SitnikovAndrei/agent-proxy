package proxy

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestCopyHeaders(t *testing.T) {
	source := http.Header{"Connection": {"X-Private, keep-alive", "X-Second"}, "X-Private": {"hidden"}, "X-Second": {"hidden"}, "Authorization": {"Bearer synthetic"}, "X-Public": {"one", "two"}}
	for _, name := range []string{"Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Trailers", "Transfer-Encoding", "Upgrade", "Host"} {
		source[name] = []string{"hidden"}
	}
	destination := make(http.Header)
	CopyHeaders(destination, source)
	expected := http.Header{"Authorization": {"Bearer synthetic"}, "X-Public": {"one", "two"}}
	if !reflect.DeepEqual(destination, expected) {
		t.Fatalf("headers=%v", destination)
	}
	source["X-Public"][0] = "changed"
	if destination.Get("X-Public") != "one" {
		t.Fatal("header values alias source")
	}
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error { b.closed = true; return nil }

func TestDecodeGzipResponse(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("data: event\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	original := &closeTrackingBody{Reader: bytes.NewReader(compressed.Bytes())}
	response := &http.Response{Header: http.Header{"Content-Encoding": {" GZip "}, "Content-Length": {"100"}}, Body: original, ContentLength: 100}
	if err := DecodeGzipResponse(response); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "data: event\n\n" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Length") != "" || response.ContentLength != -1 {
		t.Fatalf("invalid decoded headers: %v", response)
	}
	if err := response.Body.Close(); err != nil || !original.closed {
		t.Fatalf("closed=%v err=%v", original.closed, err)
	}
	invalid := &http.Response{Header: http.Header{"Content-Encoding": {"gzip"}}, Body: io.NopCloser(strings.NewReader("invalid"))}
	if err := DecodeGzipResponse(invalid); err == nil {
		t.Fatal("invalid gzip accepted")
	}
	plain := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader("plain"))}
	before := plain.Body
	if err := DecodeGzipResponse(plain); err != nil || plain.Body != before {
		t.Fatal("plain body changed")
	}
}

type orderedStreamWriter struct {
	*httptest.ResponseRecorder
	steps *[]string
	fail  bool
}

func (w orderedStreamWriter) Write(data []byte) (int, error) {
	*w.steps = append(*w.steps, "write:"+string(data))
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(data)
}
func (w orderedStreamWriter) Flush() {
	*w.steps = append(*w.steps, "flush")
	w.ResponseRecorder.Flush()
}

func TestCopyStreamOrder(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var steps []string
		writer := orderedStreamWriter{httptest.NewRecorder(), &steps, fail}
		err := CopyStream(writer, strings.NewReader("event"), func(data []byte) { steps = append(steps, "consume:"+string(data)) })
		expected := []string{"consume:event", "write:event"}
		if fail {
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal(err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected, "flush")
			if !writer.Flushed {
				t.Fatal("missing flush")
			}
		}
		if !reflect.DeepEqual(steps, expected) {
			t.Fatalf("steps=%v", steps)
		}
	}
}

func TestTransportFailure(t *testing.T) {
	cause := errors.New("connection refused")
	wrapped := &url.Error{Op: "Post", URL: "https://upstream.invalid/path?token=synthetic", Err: cause}
	err := TransportFailure(wrapped)
	if err != cause || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "upstream.invalid") {
		t.Fatalf("transport error=%v", err)
	}
	if TransportFailure(cause) != cause {
		t.Fatal("plain cause replaced")
	}
}
