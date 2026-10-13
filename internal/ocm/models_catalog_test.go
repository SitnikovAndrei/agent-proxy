package ocm

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompatibleModelsPreservesNativeCatalog(t *testing.T) {
	body := []byte(`{
		"object":"native", "data":["native"], "extra":{"version":2,"unknown":null},
		"models":[
			{"slug":"model-a", "supports_reasoning_effort_updates":false,
			 "default_reasoning_level":"medium", "default_reasoning_effort":"high",
			 "supported_reasoning_levels":[{"effort":"low","description":"fast","extra":42},{"effort":"low"}],
			 "reasoningEfforts":{"provider":"value"}, "unknown":{"nested":[1,null]}},
			{"slug":"model-b", "supported_reasoning_levels":"future-format", "default_reasoning_level":123},
			{"slug":"model-c"}
		]
	}`)
	result, err := compatibleModelsResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	var original, converted map[string]json.RawMessage
	if err := json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result, &converted); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"models", "extra"} {
		assertCatalogJSONEqual(t, original[field], converted[field])
	}
	if string(converted["object"]) != `"list"` {
		t.Fatalf("object = %s", converted["object"])
	}
	assertCatalogJSONEqual(t, converted["data"], []byte(`[
		{"id":"model-a","object":"model","created":0,"owned_by":"openai"},
		{"id":"model-b","object":"model","created":0,"owned_by":"openai"},
		{"id":"model-c","object":"model","created":0,"owned_by":"openai"}
	]`))
}

func TestCompatibleModelsEmptyCatalog(t *testing.T) {
	result, err := compatibleModelsResponse([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	assertCatalogJSONEqual(t, result, []byte(`{"models":[],"object":"list","data":[]}`))
}

func TestCompatibleModelsRejectsInvalidStructure(t *testing.T) {
	for _, body := range []string{
		`{`, `null`, `[]`, `{}`, `{"models":null}`, `{"models":{}}`,
		`{"models":[null]}`, `{"models":[[]]}`, `{"models":["model"]}`,
		`{"models":[{}]}`, `{"models":[{"slug":" "}]}`, `{"models":[{"slug":123}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := compatibleModelsResponse([]byte(body)); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func assertCatalogJSONEqual(t *testing.T, actual, expected []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(actual, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("JSON mismatch:\nactual: %s\nexpected: %s", actual, expected)
	}
}
