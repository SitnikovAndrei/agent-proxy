package ocm

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ocm/internal/proxy"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func TestWebSocketPipelinedFinalUsage(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "request models", true: "upstream model"}[override], func(t *testing.T) {
			first := strings.ReplaceAll(completedEvent, "response.completed", "response.incomplete")
			first = strings.ReplaceAll(first, `"model":"gpt-5.4",`, "")
			first = strings.ReplaceAll(first, "resp_test", "resp_first")
			second := strings.ReplaceAll(first, "response.incomplete", "response.failed")
			second = strings.ReplaceAll(second, "resp_first", "resp_second")
			expectedSecond := "gpt-5.2"
			if override {
				second = strings.Replace(second, `"response":{`, `"response":{"model":"gpt-5.4",`, 1)
				expectedSecond = "gpt-5.4"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != "configured-agent" || r.Header.Get("Originator") != "configured-origin" || r.Header.Get("Accept-Encoding") != "" {
					t.Errorf("handshake headers: %v", r.Header)
				}
				conn, _, _, err := ws.UpgradeHTTP(r, w)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				for range 2 {
					if _, _, err := wsutil.ReadClientData(conn); err != nil {
						t.Error(err)
						return
					}
				}
				for _, event := range []string{first, first, second} {
					if err := wsutil.WriteServerMessage(conn, ws.OpText, []byte(event)); err != nil {
						t.Error(err)
						return
					}
				}
				_, _, _ = wsutil.ReadClientData(conn)
			}))
			defer upstream.Close()
			service := startTestService(t, upstream.URL, true, io.Discard)
			service.httpHeaders.Set("User-Agent", "configured-agent")
			service.httpHeaders.Set("Originator", "configured-origin")
			dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Authorization": []string{"Bearer client-key"}, "User-Agent": []string{"client-agent"}, "Originator": []string{"client-origin"}, "Accept-Encoding": []string{"br"}})}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, buffered, _, err := dialer.Dial(ctx, "ws://"+service.listener.Addr().String()+"/v1/responses")
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			for _, model := range []string{"gpt-5.4", "gpt-5.2"} {
				if err := wsutil.WriteClientMessage(conn, ws.OpText, []byte(`{"type":"response.create","model":"`+model+`"}`)); err != nil {
					t.Fatal(err)
				}
			}
			var rw io.ReadWriter = conn
			if buffered != nil {
				rw = struct {
					io.Reader
					io.Writer
				}{buffered, conn}
			}
			for range 3 {
				if _, _, err := wsutil.ReadServerData(rw); err != nil {
					t.Fatal(err)
				}
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", testUsagePath(service))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows, err := db.Query("SELECT model,input_tokens FROM usage_records ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for _, want := range []string{"gpt-5.4", expectedSecond} {
				if !rows.Next() {
					t.Fatal("missing usage record")
				}
				var model string
				var input int64
				if err := rows.Scan(&model, &input); err != nil {
					t.Fatal(err)
				}
				if model != want || input != 90 {
					t.Fatalf("model=%s input=%d want=%s", model, input, want)
				}
			}
			if rows.Next() {
				t.Fatal("duplicate final event recorded twice")
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWebSocketMessageLimit(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		var source io.Reader
		var header bytes.Buffer
		if !fragmented {
			if err := ws.WriteHeader(&header, ws.Header{Fin: true, OpCode: ws.OpText, Masked: true, Length: proxy.MaxRequestBodyBytes + 1}); err != nil {
				t.Fatal(err)
			}
			source = &header
		} else {
			if err := ws.WriteHeader(&header, ws.Header{Fin: false, OpCode: ws.OpText, Masked: true, Length: proxy.MaxRequestBodyBytes / 2}); err != nil {
				t.Fatal(err)
			}
			var continuation bytes.Buffer
			if err := ws.WriteHeader(&continuation, ws.Header{Fin: true, OpCode: ws.OpContinuation, Masked: true, Length: proxy.MaxRequestBodyBytes/2 + 1}); err != nil {
				t.Fatal(err)
			}
			source = io.MultiReader(&header, io.LimitReader(repeatingBody{}, proxy.MaxRequestBodyBytes/2), &continuation, io.LimitReader(repeatingBody{}, proxy.MaxRequestBodyBytes/2+1))
		}
		_, _, err := readLimitedClientData(struct {
			io.Reader
			io.Writer
		}{source, io.Discard})
		if !errors.Is(err, wsutil.ErrFrameTooLarge) {
			t.Fatalf("fragmented=%v error=%v", fragmented, err)
		}
	}
}

func TestWebSocketControlAndFragmentedData(t *testing.T) {
	var source, output bytes.Buffer
	for _, frame := range []ws.Frame{
		ws.NewPingFrame([]byte("ping")),
		ws.NewFrame(ws.OpText, false, []byte("hel")),
		ws.NewPingFrame([]byte("ping")),
		ws.NewFrame(ws.OpContinuation, true, []byte("lo")),
	} {
		if err := ws.WriteFrame(&source, ws.MaskFrame(frame)); err != nil {
			t.Fatal(err)
		}
	}
	data, op, err := readLimitedClientData(struct {
		io.Reader
		io.Writer
	}{&source, &output})
	if err != nil || op != ws.OpText || string(data) != "hello" {
		t.Fatalf("data=%q op=%v err=%v", data, op, err)
	}
	for range 2 {
		frame, err := ws.ReadFrame(&output)
		if err != nil || frame.Header.OpCode != ws.OpPong || string(frame.Payload) != "ping" {
			t.Fatalf("pong=%+v err=%v", frame, err)
		}
	}
}

func TestOversizedWebSocketClosesSession(t *testing.T) {
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _, err = wsutil.ReadClientData(conn)
		if err == nil {
			t.Error("oversized message reached upstream")
		}
		close(upstreamClosed)
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Authorization": []string{"Bearer client-key"}})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, _, err := dialer.Dial(ctx, "ws://"+service.listener.Addr().String()+"/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := ws.WriteHeader(conn, ws.Header{Fin: true, OpCode: ws.OpText, Masked: true, Length: proxy.MaxRequestBodyBytes + 1}); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if _, err := conn.Read(data[:]); err == nil {
		t.Fatal("client session remained open")
	}
	select {
	case <-upstreamClosed:
	case <-ctx.Done():
		t.Fatal("upstream session remained open")
	}
}
