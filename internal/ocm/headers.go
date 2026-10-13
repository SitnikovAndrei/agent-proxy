package ocm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type errorResponse struct {
	Error errorDetails `json:"error"`
}

type errorDetails struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, r *http.Request, statusCode int, errorType string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	json.NewEncoder(w).Encode(errorResponse{
		Error: errorDetails{
			Type:    errorType,
			Message: message,
		},
	})
}

func normalizeRateLimitIdentifier(limitIdentifier string) string {
	trimmedIdentifier := strings.TrimSpace(strings.ToLower(limitIdentifier))
	if trimmedIdentifier == "" {
		return ""
	}
	return strings.ReplaceAll(trimmedIdentifier, "_", "-")
}

func parseInt64Header(headers http.Header, headerName string) (int64, bool) {
	headerValue := strings.TrimSpace(headers.Get(headerName))
	if headerValue == "" {
		return 0, false
	}
	parsedValue, parseError := strconv.ParseInt(headerValue, 10, 64)
	if parseError != nil {
		return 0, false
	}
	return parsedValue, true
}

func weeklyCycleHintForLimit(headers http.Header, limitIdentifier string) *WeeklyCycleHint {
	normalizedLimitIdentifier := normalizeRateLimitIdentifier(limitIdentifier)
	if normalizedLimitIdentifier == "" {
		return nil
	}

	windowHeader := "x-" + normalizedLimitIdentifier + "-secondary-window-minutes"
	resetHeader := "x-" + normalizedLimitIdentifier + "-secondary-reset-at"

	windowMinutes, hasWindowMinutes := parseInt64Header(headers, windowHeader)
	resetAtUnix, hasResetAt := parseInt64Header(headers, resetHeader)
	if !hasWindowMinutes || !hasResetAt || windowMinutes <= 0 || resetAtUnix <= 0 {
		return nil
	}

	return &WeeklyCycleHint{
		WindowMinutes: windowMinutes,
		ResetAt:       time.Unix(resetAtUnix, 0).UTC(),
	}
}

func extractWeeklyCycleHint(headers http.Header) *WeeklyCycleHint {
	activeLimitIdentifier := normalizeRateLimitIdentifier(headers.Get("x-codex-active-limit"))
	if activeLimitIdentifier != "" {
		if activeHint := weeklyCycleHintForLimit(headers, activeLimitIdentifier); activeHint != nil {
			return activeHint
		}
	}
	return weeklyCycleHintForLimit(headers, "codex")
}
