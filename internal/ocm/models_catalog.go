package ocm

import (
	"encoding/json"
	"errors"
	"strings"
)

type compatibleModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
	reasoningMetadata
}

type reasoningEffort struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type reasoningMetadata struct {
	SupportsReasoningEffortUpdates bool              `json:"supports_reasoning_effort_updates"`
	DefaultReasoningLevel          *string           `json:"default_reasoning_level,omitempty"`
	DefaultReasoningEffort         *string           `json:"default_reasoning_effort,omitempty"`
	SupportedReasoningLevels       []reasoningEffort `json:"supported_reasoning_levels"`
	ReasoningEfforts               map[string]string `json:"reasoningEfforts"`
}

type catalogModel struct {
	Slug                     string            `json:"slug"`
	DefaultReasoningLevel    *string           `json:"default_reasoning_level"`
	DefaultReasoningEffort   *string           `json:"default_reasoning_effort"`
	SupportedReasoningLevels []reasoningEffort `json:"supported_reasoning_levels"`
}

func compatibleModelsResponse(body []byte) ([]byte, error) {
	// Raw JSON preserves upstream fields outside the compatibility metadata.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, err
	}
	if models == nil {
		return nil, errors.New("model catalog must contain a models array")
	}
	data := make([]compatibleModel, 0, len(models))
	for index, raw := range models {
		native, compatible, err := compatibleCatalogModel(raw)
		if err != nil {
			return nil, err
		}
		models[index] = native
		data = append(data, compatible)
	}
	encodedData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	encodedModels, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	envelope["object"] = json.RawMessage(`"list"`)
	envelope["data"] = encodedData
	envelope["models"] = encodedModels
	return json.Marshal(envelope)
}

func compatibleCatalogModel(raw json.RawMessage) (json.RawMessage, compatibleModel, error) {
	var model catalogModel
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, compatibleModel{}, err
	}
	if strings.TrimSpace(model.Slug) == "" {
		return nil, compatibleModel{}, errors.New("model catalog contains an empty model slug")
	}
	metadata, err := model.reasoning()
	if err != nil {
		return nil, compatibleModel{}, err
	}
	native, err := enrichCatalogModel(raw, metadata)
	if err != nil {
		return nil, compatibleModel{}, err
	}
	// The Codex catalog has no creation timestamp; zero represents unavailable.
	return native, compatibleModel{
		ID: model.Slug, Object: "model", Created: 0, OwnedBy: "openai", reasoningMetadata: metadata,
	}, nil
}

func (model catalogModel) reasoning() (reasoningMetadata, error) {
	levels := make([]reasoningEffort, 0, len(model.SupportedReasoningLevels))
	efforts := make(map[string]string, len(model.SupportedReasoningLevels))
	for _, level := range model.SupportedReasoningLevels {
		if strings.TrimSpace(level.Effort) == "" {
			return reasoningMetadata{}, errors.New("model catalog contains an empty reasoning effort")
		}
		if _, duplicate := efforts[level.Effort]; duplicate {
			return reasoningMetadata{}, errors.New("model catalog contains a duplicate reasoning effort")
		}
		efforts[level.Effort] = level.Effort
		levels = append(levels, level)
	}
	defaultLevel, err := model.defaultReasoning(efforts)
	if err != nil {
		return reasoningMetadata{}, err
	}
	return reasoningMetadata{
		SupportsReasoningEffortUpdates: len(levels) > 0,
		DefaultReasoningLevel:          defaultLevel, DefaultReasoningEffort: defaultLevel,
		SupportedReasoningLevels: levels, ReasoningEfforts: efforts,
	}, nil
}

func (model catalogModel) defaultReasoning(efforts map[string]string) (*string, error) {
	level, effort := model.DefaultReasoningLevel, model.DefaultReasoningEffort
	if level != nil && effort != nil && *level != *effort {
		return nil, errors.New("model catalog contains conflicting default reasoning values")
	}
	if level == nil {
		level = effort
	}
	if level != nil {
		if _, supported := efforts[*level]; !supported {
			return nil, errors.New("model catalog default reasoning value is not supported")
		}
	}
	return level, nil
}

func enrichCatalogModel(raw json.RawMessage, metadata reasoningMetadata) (json.RawMessage, error) {
	var native map[string]json.RawMessage
	if err := json.Unmarshal(raw, &native); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	// Preserve all provider-specific fields in the original level descriptors.
	if levels := native["supported_reasoning_levels"]; len(levels) > 0 && string(levels) != "null" {
		delete(fields, "supported_reasoning_levels")
	}
	delete(native, "default_reasoning_level")
	delete(native, "default_reasoning_effort")
	for key, value := range fields {
		native[key] = value
	}
	return json.Marshal(native)
}
