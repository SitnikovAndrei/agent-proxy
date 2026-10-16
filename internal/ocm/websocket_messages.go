package ocm

import (
	"io"
	"sync"

	"ocm/internal/proxy"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type webSocketWriter struct {
	mutex sync.Mutex
	dst   io.Writer
	state ws.State
}

func (w *webSocketWriter) writeMessage(op ws.OpCode, data []byte) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return wsutil.WriteMessage(w.dst, w.state, op, data)
}

func (w *webSocketWriter) controlHandler() wsutil.FrameHandlerFunc {
	handler := wsutil.ControlFrameHandler(w.dst, w.state)
	return func(header ws.Header, source io.Reader) error {
		// A control reply can also write its header and payload separately.
		w.mutex.Lock()
		defer w.mutex.Unlock()
		return handler(header, source)
	}
}

type responseModelQueue struct {
	mutex  sync.Mutex
	models []string
}

func (q *responseModelQueue) push(model string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.models = append(q.models, model)
}

func (q *responseModelQueue) pop() string {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if len(q.models) == 0 {
		return ""
	}
	model := q.models[0]
	q.models[0] = ""
	q.models = q.models[1:]
	return model
}

// Bound both individual frames and the aggregate of fragmented messages.
func readLimitedClientData(source io.Reader, writer *webSocketWriter) ([]byte, ws.OpCode, error) {
	control := writer.controlHandler()
	reader := wsutil.Reader{Source: source, State: ws.StateServerSide, CheckUTF8: true,
		MaxFrameSize: proxy.MaxRequestBodyBytes, OnIntermediate: control}
	for {
		header, err := reader.NextFrame()
		if err != nil {
			return nil, 0, err
		}
		if header.OpCode.IsControl() {
			if err := control(header, &reader); err != nil {
				return nil, 0, err
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(&reader, proxy.MaxRequestBodyBytes+1))
		if err != nil {
			return nil, 0, err
		}
		if len(data) > proxy.MaxRequestBodyBytes {
			return nil, 0, wsutil.ErrFrameTooLarge
		}
		return data, header.OpCode, nil
	}
}

func readUpstreamWebSocketData(source io.Reader, writer *webSocketWriter) ([]byte, ws.OpCode, error) {
	control := writer.controlHandler()
	reader := wsutil.Reader{Source: source, State: ws.StateClientSide, CheckUTF8: true,
		OnIntermediate: control}
	for {
		header, err := reader.NextFrame()
		if err != nil {
			return nil, 0, err
		}
		if header.OpCode.IsControl() {
			if err := control(header, &reader); err != nil {
				return nil, 0, err
			}
			continue
		}
		data, err := io.ReadAll(&reader)
		return data, header.OpCode, err
	}
}
