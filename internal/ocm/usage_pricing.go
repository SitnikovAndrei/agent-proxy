package ocm

import (
	"regexp"
	"strings"

	"ocm/internal/statistics"
)

type ModelPricing struct {
	InputPrice       float64
	OutputPrice      float64
	CachedInputPrice float64
}

type modelFamily struct {
	pattern        *regexp.Regexp
	pricing        ModelPricing
	premiumPricing *ModelPricing
}

const (
	contextWindowStandard   = 272000
	contextWindowPremium    = 1050000
	premiumContextThreshold = 272000
)

func modelFamiliesForTier(serviceTier statistics.ServiceTier) []modelFamily {
	switch serviceTier {
	case statistics.TierFlex:
		return flexModelFamilies
	case statistics.TierPriority:
		return priorityModelFamilies
	case statistics.TierDefault, statistics.TierAuto, statistics.TierScale:
		// Preserve the existing standard-price estimate for auto and scale.
		return standardModelFamilies
	default:
		panic("invalid ServiceTier in pricing")
	}
}

func findPricingInFamilies(model string, contextWindow int, modelFamilies []modelFamily) (ModelPricing, bool) {
	isPremium := contextWindow >= contextWindowPremium
	for _, family := range modelFamilies {
		if family.pattern.MatchString(model) {
			if isPremium && family.premiumPricing != nil {
				return *family.premiumPricing, true
			}
			return family.pricing, true
		}
	}
	return ModelPricing{}, false
}

func hasPremiumPricingInFamilies(model string, modelFamilies []modelFamily) bool {
	for _, family := range modelFamilies {
		if family.pattern.MatchString(model) {
			return family.premiumPricing != nil
		}
	}
	return false
}

func getPricing(model string, serviceTier statistics.ServiceTier, contextWindow int) ModelPricing {
	families := modelFamiliesForTier(serviceTier)

	if pricing, found := findPricingInFamilies(model, contextWindow, families); found {
		return pricing
	}

	normalizedModel := normalizeGPT5Model(model)
	if normalizedModel != model {
		if pricing, found := findPricingInFamilies(normalizedModel, contextWindow, families); found {
			return pricing
		}
	}

	if serviceTier == statistics.TierFlex || serviceTier == statistics.TierPriority {
		if pricing, found := findPricingInFamilies(model, contextWindow, standardModelFamilies); found {
			return pricing
		}
		if normalizedModel != model {
			if pricing, found := findPricingInFamilies(normalizedModel, contextWindow, standardModelFamilies); found {
				return pricing
			}
		}
	}

	return gpt4oPricing
}

func detectContextWindow(model string, serviceTier statistics.ServiceTier, inputTokens int64) int {
	if inputTokens <= premiumContextThreshold {
		return contextWindowStandard
	}
	families := modelFamiliesForTier(serviceTier)
	if hasPremiumPricingInFamilies(model, families) {
		return contextWindowPremium
	}
	normalizedModel := normalizeGPT5Model(model)
	if normalizedModel != model && hasPremiumPricingInFamilies(normalizedModel, families) {
		return contextWindowPremium
	}
	if serviceTier == statistics.TierFlex || serviceTier == statistics.TierPriority {
		if hasPremiumPricingInFamilies(model, standardModelFamilies) {
			return contextWindowPremium
		}
		if normalizedModel != model && hasPremiumPricingInFamilies(normalizedModel, standardModelFamilies) {
			return contextWindowPremium
		}
	}
	return contextWindowStandard
}

func normalizeGPT5Model(model string) string {
	if !strings.HasPrefix(model, "gpt-5.") {
		return model
	}

	switch {
	case strings.Contains(model, "-codex-mini"):
		return "gpt-5.1-codex-mini"
	case strings.Contains(model, "-codex-max"):
		return "gpt-5.1-codex-max"
	case strings.Contains(model, "-codex"):
		return "gpt-5.3-codex"
	case strings.Contains(model, "-chat-latest"):
		return "gpt-5.2-chat-latest"
	case strings.Contains(model, "-pro"):
		return "gpt-5.4-pro"
	case strings.Contains(model, "-mini"):
		return "gpt-5-mini"
	case strings.Contains(model, "-nano"):
		return "gpt-5-nano"
	default:
		return "gpt-5.4"
	}
}

func calculateCost(stats UsageStats, model string, serviceTier statistics.ServiceTier, contextWindow int) float64 {
	pricing := getPricing(model, serviceTier, contextWindow)

	regularInputTokens := stats.InputTokens - stats.CachedTokens

	cost := (float64(regularInputTokens)*pricing.InputPrice +
		float64(stats.OutputTokens)*pricing.OutputPrice +
		float64(stats.CachedTokens)*pricing.CachedInputPrice) / 1_000_000

	return cost
}
