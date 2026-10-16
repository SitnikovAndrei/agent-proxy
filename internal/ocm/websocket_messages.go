package ocm

import (
	"io"
	"sync"

	"ocm/internal/proxy"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

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
func readLimitedClientData(rw io.ReadWriter) ([]byte, ws.OpCode, error) {
	control := wsutil.ControlFrameHandler(rw, ws.StateServerSide)
	reader := wsutil.Reader{Source: rw, State: ws.StateServerSide, CheckUTF8: true,
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
