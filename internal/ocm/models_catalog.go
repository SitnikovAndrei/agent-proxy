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
}

func compatibleModelsResponse(body []byte) ([]byte, error) {
	// Preserve the native catalog and unknown fields; adapt only the data list.
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
	for _, raw := range models {
		model, err := compatibleCatalogModel(raw)
		if err != nil {
			return nil, err
		}
		data = append(data, model)
	}
	encodedData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	envelope["object"] = json.RawMessage(`"list"`)
	envelope["data"] = encodedData
	return json.Marshal(envelope)
}

func compatibleCatalogModel(raw json.RawMessage) (compatibleModel, error) {
	var model struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return compatibleModel{}, err
	}
	if strings.TrimSpace(model.Slug) == "" {
		return compatibleModel{}, errors.New("model catalog contains an empty model slug")
	}
	// The Codex catalog has no creation timestamp; zero represents unavailable.
	return compatibleModel{
		ID: model.Slug, Object: "model", Created: 0, OwnedBy: "openai",
	}, nil
}
