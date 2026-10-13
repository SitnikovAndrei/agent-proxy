package proxy

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
)

func DecodeGzipResponse(response *http.Response) error {
	if !strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Encoding")), "gzip") {
		return nil
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		return err
	}
	response.Body = &gzipResponseBody{Reader: reader, compressed: response.Body}
	response.Header.Del("Content-Encoding")
	response.Header.Del("Content-Length")
	response.ContentLength = -1
	return nil
}

type gzipResponseBody struct {
	*gzip.Reader
	compressed io.Closer
}

func (b *gzipResponseBody) Close() error {
	return errors.Join(b.Reader.Close(), b.compressed.Close())
}
