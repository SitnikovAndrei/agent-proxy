package statistics

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ServiceTier uint8

const (
	TierDefault ServiceTier = iota
	TierAuto
	TierFlex
	TierPriority
	TierScale
)

func ParseServiceTier(value string) (ServiceTier, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default":
		// Responses without this optional field use the standard tier.
		return TierDefault, nil
	case "auto":
		return TierAuto, nil
	case "flex":
		return TierFlex, nil
	case "priority":
		return TierPriority, nil
	case "scale":
		return TierScale, nil
	default:
		return TierDefault, errors.New("unsupported service_tier")
	}
}

func (t ServiceTier) WireValue() (string, error) {
	switch t {
	case TierDefault:
		return "default", nil
	case TierAuto:
		return "auto", nil
	case TierFlex:
		return "flex", nil
	case TierPriority:
		return "priority", nil
	case TierScale:
		return "scale", nil
	default:
		return "", fmt.Errorf("invalid ServiceTier value: %d", t)
	}
}

func (t ServiceTier) MarshalJSON() ([]byte, error) {
	value, err := t.WireValue()
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (t *ServiceTier) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode service_tier: %w", err)
	}
	parsed, err := ParseServiceTier(value)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}
