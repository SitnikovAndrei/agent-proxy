package ocm

import (
	"time"
)

type UsageStats struct {
	RequestCount int
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
}

type WeeklyCycleHint struct {
	WindowMinutes int64
	ResetAt       time.Time
}
