package ccm

import (
	"regexp"
)

type ModelPricing struct {
	InputPrice             float64
	OutputPrice            float64
	CacheReadPrice         float64
	CacheWritePrice5Minute float64
	CacheWritePrice1Hour   float64
}

type modelFamily struct {
	pattern         *regexp.Regexp
	standardPricing ModelPricing
	premiumPricing  *ModelPricing
}

func getPricing(model string, contextWindow int) ModelPricing {
	isPremium := contextWindow >= contextWindowPremium

	for _, family := range modelFamilies {
		if family.pattern.MatchString(model) {
			if isPremium && family.premiumPricing != nil {
				return *family.premiumPricing
			}
			return family.standardPricing
		}
	}

	return sonnet4StandardPricing
}

func calculateCost(stats UsageStats, model string, contextWindow int) float64 {
	pricing := getPricing(model, contextWindow)

	cacheCreationCost := 0.0
	if stats.CacheCreation5MinuteInputTokens > 0 || stats.CacheCreation1HourInputTokens > 0 {
		cacheCreationCost = float64(stats.CacheCreation5MinuteInputTokens)*pricing.CacheWritePrice5Minute +
			float64(stats.CacheCreation1HourInputTokens)*pricing.CacheWritePrice1Hour
	} else {
		// Usage without a TTL split uses the five-minute cache write rate.
		cacheCreationCost = float64(stats.CacheCreationInputTokens) * pricing.CacheWritePrice5Minute
	}

	cost := (float64(stats.InputTokens)*pricing.InputPrice +
		float64(stats.OutputTokens)*pricing.OutputPrice +
		float64(stats.CacheReadInputTokens)*pricing.CacheReadPrice +
		cacheCreationCost) / 1_000_000

	return cost
}
