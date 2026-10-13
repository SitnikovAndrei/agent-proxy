package ccm

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	contextWindowStandard   = 200000
	contextWindowPremium    = 1000000
	premiumContextThreshold = 200000
)

func detectContextWindow(betaHeader string, totalInputTokens int64) int {
	if totalInputTokens > premiumContextThreshold {
		for feature := range strings.SplitSeq(betaHeader, ",") {
			if strings.HasPrefix(strings.TrimSpace(feature), "context-1m") {
				return contextWindowPremium
			}
		}
	}
	return contextWindowStandard
}

func weekStartUnix(headers http.Header) int64 {
	value := strings.TrimSpace(headers.Get("anthropic-ratelimit-unified-7d-reset"))
	reset, err := strconv.ParseInt(value, 10, 64)
	if err != nil || reset <= 0 {
		return 0
	}
	return time.Unix(reset, 0).UTC().Add(-7 * 24 * time.Hour).Unix()
}
