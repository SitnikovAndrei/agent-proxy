package ocm

import (
	"context"
	"errors"
	"net"
	"time"
)

const shutdownTimeout = 30 * time.Second

func (s *Service) Close() error {
	s.closeOnce.Do(func() { s.closeError = s.close() })
	return s.closeError
}

func (s *Service) close() error {
	defer s.httpClient.CloseIdleConnections()
	sessions := s.startWebSocketShutdown()
	s.cancel()
	for _, session := range sessions {
		session.Close()
	}
	var closeError error
	if s.httpServer != nil {
		closeError = s.httpServer.Close()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeError = errors.Join(closeError, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.waitForRequests(ctx); err != nil {
		return errors.Join(closeError, err)
	}

	return closeError
}

func (s *Service) registerRequest() bool {
	s.webSocketMutex.Lock()
	defer s.webSocketMutex.Unlock()
	if s.shuttingDown {
		return false
	}
	s.requestGroup.Add(1)
	return true
}

func (s *Service) waitForRequests(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.requestGroup.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) registerWebSocketSession(session *webSocketSession) bool {
	s.webSocketMutex.Lock()
	defer s.webSocketMutex.Unlock()

	if s.shuttingDown {
		return false
	}

	s.webSocketConns[session] = struct{}{}
	return true
}

func (s *Service) unregisterWebSocketSession(session *webSocketSession) {
	s.webSocketMutex.Lock()
	defer s.webSocketMutex.Unlock()
	delete(s.webSocketConns, session)
}

func (s *Service) isShuttingDown() bool {
	s.webSocketMutex.Lock()
	defer s.webSocketMutex.Unlock()
	return s.shuttingDown
}

func (s *Service) startWebSocketShutdown() []*webSocketSession {
	s.webSocketMutex.Lock()
	defer s.webSocketMutex.Unlock()

	s.shuttingDown = true

	webSocketSessions := make([]*webSocketSession, 0, len(s.webSocketConns))
	for session := range s.webSocketConns {
		webSocketSessions = append(webSocketSessions, session)
	}
	return webSocketSessions
}
