package ccm

import (
	"time"
)

type UsageStats struct {
	RequestCount                    int
	MessagesCount                   int
	InputTokens                     int64
	OutputTokens                    int64
	CacheReadInputTokens            int64
	CacheCreationInputTokens        int64
	CacheCreation5MinuteInputTokens int64
	CacheCreation1HourInputTokens   int64
}

type usageRecord struct {
	model         string
	contextWindow int
	stats         UsageStats
	username      string
	observedAt    time.Time
	weekStartUnix int64
}
