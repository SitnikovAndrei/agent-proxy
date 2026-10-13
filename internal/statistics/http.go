package statistics

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"ocm/internal/proxy"
)

type Handler struct {
	store    *Store
	users    *proxy.UserManager
	provider Provider
	logger   *slog.Logger
}

func NewHandler(store *Store, users *proxy.UserManager, provider Provider, logger *slog.Logger) (*Handler, error) {
	if store == nil || users == nil || logger == nil {
		return nil, errors.New("statistics handler dependencies are required")
	}
	if _, err := provider.wireValue(); err != nil {
		return nil, err
	}
	return &Handler{store: store, users: users, provider: provider, logger: logger}, nil
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Add("Vary", "Authorization")
	username, authorized := h.authorize(writer, request)
	if !authorized {
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		h.writeError(writer, http.StatusMethodNotAllowed, "statistics require GET")
		return
	}
	filter, err := h.requestFilter(request, username)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUserScope) {
			status = http.StatusForbidden
		}
		h.writeError(writer, status, err.Error())
		return
	}
	h.writeSummary(writer, request, filter)
}

func (h *Handler) requestFilter(request *http.Request, username string) (Filter, error) {
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return Filter{}, errors.New("invalid query encoding")
	}
	return parseFilter(values, username, h.provider)
}

func (h *Handler) writeSummary(writer http.ResponseWriter, request *http.Request, filter Filter) {
	summary, err := h.store.Summary(request.Context(), filter)
	if err != nil {
		h.logger.Error("query usage statistics", "error", err)
		h.writeError(writer, http.StatusInternalServerError, "cannot read statistics")
		return
	}
	if err := json.NewEncoder(writer).Encode(summary); err != nil {
		h.logger.Error("write usage statistics", "error", err)
	}
}

func (h *Handler) writeError(writer http.ResponseWriter, status int, message string) {
	response := struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}{}
	response.Error.Message = message
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		h.logger.Error("write statistics error", "error", err)
	}
}

func (h *Handler) authorize(writer http.ResponseWriter, request *http.Request) (string, bool) {
	username, failure := h.users.AuthorizeStatistics(request)
	if failure != proxy.Authenticated {
		status := http.StatusUnauthorized
		if failure == proxy.InvalidStatisticsUser {
			status = http.StatusForbidden
		} else {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="statistics"`)
		}
		h.writeError(writer, status, failure.Message())
		return "", false
	}
	return username, true
}
