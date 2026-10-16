package ocm

import (
	"context"
	stdTLS "crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"ocm/internal/proxy"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/openai/openai-go/v3/responses"
)

type webSocketSession struct {
	clientConn   net.Conn
	upstreamConn net.Conn
	closeOnce    sync.Once
}

func (s *webSocketSession) Close() {
	s.closeOnce.Do(func() {
		s.clientConn.Close()
		s.upstreamConn.Close()
	})
}

func buildUpstreamWebSocketURL(baseURL string, proxyPath string) string {
	upstreamURL := baseURL
	if strings.HasPrefix(upstreamURL, "https://") {
		upstreamURL = "wss://" + upstreamURL[len("https://"):]
	} else if strings.HasPrefix(upstreamURL, "http://") {
		upstreamURL = "ws://" + upstreamURL[len("http://"):]
	}
	return upstreamURL + proxyPath
}

func isForwardableResponseHeader(key string) bool {
	lowerKey := strings.ToLower(key)
	switch {
	case strings.HasPrefix(lowerKey, "x-codex-"):
		return true
	case strings.HasPrefix(lowerKey, "x-reasoning"):
		return true
	case lowerKey == "openai-model":
		return true
	case strings.Contains(lowerKey, "-secondary-"):
		return true
	default:
		return false
	}
}

func (s *Service) handleWebSocket(w http.ResponseWriter, r *http.Request, proxyPath string, username string, credentials *oauthCredentials) {
	upstreamURL := buildUpstreamWebSocketURL(s.baseURL(credentials), proxyPath)
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upstreamHeaders := s.upstreamWebSocketHeaders(r.Header, credentials)

	upstreamResponseHeaders := make(http.Header)
	upstreamDialer := ws.Dialer{
		NetDial:   s.dialer.DialContext,
		TLSConfig: &stdTLS.Config{},
		Header:    ws.HandshakeHeaderHTTP(upstreamHeaders),
		OnHeader: func(key, value []byte) error {
			upstreamResponseHeaders.Add(string(key), string(value))
			return nil
		},
	}

	upstreamConn, upstreamBufferedReader, _, err := upstreamDialer.Dial(r.Context(), upstreamURL)
	if err != nil {
		s.logger.Error("dial upstream websocket", "error", proxy.TransportFailure(err))
		writeJSONError(w, r, http.StatusBadGateway, "api_error", "upstream websocket connection failed")
		return
	}

	weeklyCycleHint := extractWeeklyCycleHint(upstreamResponseHeaders)

	clientResponseHeaders := make(http.Header)
	for key, values := range upstreamResponseHeaders {
		if isForwardableResponseHeader(key) {
			clientResponseHeaders[key] = values
		}
	}

	clientUpgrader := ws.HTTPUpgrader{
		Header: clientResponseHeaders,
	}
	if s.lifecycle.IsStopping() {
		upstreamConn.Close()
		writeJSONError(w, r, http.StatusServiceUnavailable, "api_error", "service is shutting down")
		return
	}
	clientConn, _, _, err := clientUpgrader.Upgrade(r, w)
	if err != nil {
		s.logger.Error("upgrade client websocket", "error", err)
		upstreamConn.Close()
		return
	}
	session := &webSocketSession{
		clientConn:   clientConn,
		upstreamConn: upstreamConn,
	}
	if !s.lifecycle.Register(session) {
		session.Close()
		return
	}
	defer s.lifecycle.Unregister(session)

	var upstreamReadWriter io.ReadWriter
	if upstreamBufferedReader != nil {
		upstreamReadWriter = struct {
			io.Reader
			io.Writer
		}{upstreamBufferedReader, upstreamConn}
	} else {
		upstreamReadWriter = upstreamConn
	}

	models := &responseModelQueue{}
	var waitGroup sync.WaitGroup

	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		defer session.Close()
		s.proxyWebSocketClientToUpstream(clientConn, upstreamConn, models)
	}()
	go func() {
		defer waitGroup.Done()
		defer session.Close()
		s.proxyWebSocketUpstreamToClient(upstreamReadWriter, clientConn, models, username, weeklyCycleHint)
	}()
	waitGroup.Wait()
}

func (s *Service) proxyWebSocketClientToUpstream(clientConn net.Conn, upstreamConn net.Conn, models *responseModelQueue) {
	for {
		data, opCode, err := readLimitedClientData(clientConn)
		if err != nil {
			if !isClosedOrCanceled(err) {
				s.logger.Debug("read client websocket", "error", err)
			}
			return
		}

		if opCode == ws.OpText && s.usageTracker != nil {
			var request struct {
				Type  string `json:"type"`
				Model string `json:"model"`
			}
			if json.Unmarshal(data, &request) == nil && request.Type == "response.create" {
				models.push(request.Model)
			}
		}

		err = wsutil.WriteClientMessage(upstreamConn, opCode, data)
		if err != nil {
			if !isClosedOrCanceled(err) {
				s.logger.Debug("write upstream websocket", "error", err)
			}
			return
		}
	}
}

func (s *Service) proxyWebSocketUpstreamToClient(upstreamReadWriter io.ReadWriter, clientConn net.Conn, models *responseModelQueue, username string, weeklyCycleHint *WeeklyCycleHint) {
	finalized := make(map[string]struct{})
	for {
		data, opCode, err := wsutil.ReadServerData(upstreamReadWriter)
		if err != nil {
			if !isClosedOrCanceled(err) {
				s.logger.Debug("read upstream websocket", "error", err)
			}
			return
		}

		if opCode == ws.OpText && s.usageTracker != nil {
			var event struct {
				Type     string             `json:"type"`
				Response responses.Response `json:"response"`
			}
			var kind struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &kind) == nil && isFinalResponseEvent(kind.Type) && json.Unmarshal(data, &event) == nil {
				_, duplicate := finalized[event.Response.ID]
				if event.Response.ID == "" || !duplicate {
					if event.Response.ID != "" {
						finalized[event.Response.ID] = struct{}{}
					}
					requestModel := models.pop()
					usage, err := usageFromResponse(event.Response)
					if err != nil {
						s.logger.Error("parse websocket usage", "error", err)
					} else {
						s.recordUsage(usage, requestModel, username, weeklyCycleHint)
					}
				}
			}
		}

		err = wsutil.WriteServerMessage(clientConn, opCode, data)
		if err != nil {
			if !isClosedOrCanceled(err) {
				s.logger.Debug("write client websocket", "error", err)
			}
			return
		}
	}
}

func isClosedOrCanceled(err error) bool {
	var closed wsutil.ClosedError
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.As(err, &closed) || (err != nil && strings.Contains(err.Error(), "use of closed network connection"))
}
