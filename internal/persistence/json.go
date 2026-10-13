package persistence

import (
	"bytes"
	"encoding/json"
	"errors"
)

// CanonicalJSON sorts object keys at every depth for file revision comparisons.
func CanonicalJSON(data json.RawMessage) ([]byte, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty JSON value")
	}
	switch data[0] {
	case '{':
		return canonicalObject(data)
	case '[':
		return canonicalArray(data)
	case '"':
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		return json.Marshal(value)
	default:
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			return nil, err
		}
		return compact.Bytes(), nil
	}
}

func canonicalObject(data json.RawMessage) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	for key, value := range object {
		canonical, err := CanonicalJSON(value)
		if err != nil {
			return nil, err
		}
		object[key] = canonical
	}
	return json.Marshal(object)
}

func canonicalArray(data json.RawMessage) ([]byte, error) {
	var array []json.RawMessage
	if err := json.Unmarshal(data, &array); err != nil {
		return nil, err
	}
	for index, value := range array {
		canonical, err := CanonicalJSON(value)
		if err != nil {
			return nil, err
		}
		array[index] = canonical
	}
	return json.Marshal(array)
}
