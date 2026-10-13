package ccm

import (
	"time"
)

type tokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheCreation            struct {
		Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

type responseMessage struct {
	Model string     `json:"model"`
	Usage tokenUsage `json:"usage"`
}

type usageDelta struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens *int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens *int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (u *tokenUsage) apply(delta usageDelta) {
	if delta.InputTokens != nil {
		u.InputTokens = *delta.InputTokens
	}
	if delta.OutputTokens != nil {
		u.OutputTokens = *delta.OutputTokens
	}
	if delta.CacheReadInputTokens != nil {
		u.CacheReadInputTokens = *delta.CacheReadInputTokens
	}
	if delta.CacheCreationInputTokens != nil {
		u.CacheCreationInputTokens = *delta.CacheCreationInputTokens
	}
	if delta.CacheCreation != nil {
		if delta.CacheCreation.Ephemeral5mInputTokens != nil {
			u.CacheCreation.Ephemeral5mInputTokens = *delta.CacheCreation.Ephemeral5mInputTokens
		}
		if delta.CacheCreation.Ephemeral1hInputTokens != nil {
			u.CacheCreation.Ephemeral1hInputTokens = *delta.CacheCreation.Ephemeral1hInputTokens
		}
	}
}

func (s *Service) recordUsage(message responseMessage, metadata requestMetadata, weekStart int64) {
	usage := message.Usage
	if message.Model == "" {
		message.Model = metadata.model
	}
	cacheCreation := usage.CacheCreationInputTokens
	if cacheCreation == 0 {
		cacheCreation = usage.CacheCreation.Ephemeral5mInputTokens + usage.CacheCreation.Ephemeral1hInputTokens
	}
	totalInput := usage.InputTokens + usage.CacheReadInputTokens + cacheCreation
	if totalInput == 0 && usage.OutputTokens == 0 {
		return
	}
	record := usageRecord{
		model: message.Model, contextWindow: detectContextWindow(metadata.beta, totalInput), username: metadata.username,
		observedAt: time.Now(), weekStartUnix: weekStart,
		stats: UsageStats{RequestCount: 1, MessagesCount: metadata.messagesCount, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CacheReadInputTokens: usage.CacheReadInputTokens, CacheCreationInputTokens: cacheCreation,
			CacheCreation5MinuteInputTokens: usage.CacheCreation.Ephemeral5mInputTokens, CacheCreation1HourInputTokens: usage.CacheCreation.Ephemeral1hInputTokens},
	}
	if err := s.usage.record(record); err != nil {
		s.logger.Error("record Claude usage", "error", err)
	}
}
