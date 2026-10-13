package proxy

import (
	"net/http"
	"strings"
	"sync"

	"ocm/internal/config"
)

type AuthFailure uint8

const (
	Authenticated AuthFailure = iota
	MissingAuthorization
	InvalidAuthorization
	UnknownToken
	InvalidStatisticsUser
)

func (f AuthFailure) Message() string {
	switch f {
	case Authenticated:
		return "authenticated"
	case MissingAuthorization:
		return "missing api key"
	case InvalidAuthorization:
		return "invalid api key format"
	case UnknownToken:
		return "invalid api key"
	case InvalidStatisticsUser:
		return "statistics require a unique nonempty user name"
	default:
		panic("invalid authentication result")
	}
}

type UserManager struct {
	mutex  sync.RWMutex
	tokens map[string]string
}

func (m *UserManager) UpdateUsers(users []config.User) {
	tokens := make(map[string]string, len(users))
	for _, user := range users {
		tokens[user.Token] = user.Name
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.tokens = tokens
}

func (m *UserManager) Authorize(request *http.Request) (string, AuthFailure) {
	return m.authorize(request, false)
}

func (m *UserManager) AuthorizeStatistics(request *http.Request) (string, AuthFailure) {
	return m.authorize(request, true)
}

func (m *UserManager) authorize(request *http.Request, statistics bool) (string, AuthFailure) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if len(m.tokens) == 0 {
		if statistics {
			return "", MissingAuthorization
		}
		return "", Authenticated
	}
	header := request.Header.Get("Authorization")
	if header == "" {
		return "", MissingAuthorization
	}
	token, valid := strings.CutPrefix(header, "Bearer ")
	if !valid {
		return "", InvalidAuthorization
	}
	username, found := m.tokens[token]
	if !found {
		return "", UnknownToken
	}
	if statistics && !m.uniqueName(username) {
		return "", InvalidStatisticsUser
	}
	return username, Authenticated
}

// Called while holding the read lock.
func (m *UserManager) uniqueName(username string) bool {
	if strings.TrimSpace(username) == "" {
		return false
	}
	count := 0
	for _, name := range m.tokens {
		if name == username {
			count++
		}
	}
	return count == 1
}
