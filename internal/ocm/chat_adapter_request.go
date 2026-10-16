package ocm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type chatAdapterRequest struct {
	body         []byte
	model        string
	stream       bool
	includeUsage bool
}

type adapterFunction struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type adapterTextFormat struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

func convertChatRequest(body []byte) (chatAdapterRequest, error) {
	var fields map[string]json.RawMessage
	result := chatAdapterRequest{}
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return result, errors.New("request body must be a JSON object")
	}
	if raw, ok := fields["stream"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &result.stream) != nil {
			return result, errors.New("stream must be a boolean")
		}
	}
	if raw, ok := fields["stream_options"]; ok {
		var options struct {
			IncludeUsage json.RawMessage `json:"include_usage"`
		}
		if json.Unmarshal(raw, &options) != nil {
			return result, errors.New("stream_options.include_usage must be a boolean")
		}
		if options.IncludeUsage != nil {
			if bytes.Equal(options.IncludeUsage, []byte("null")) || json.Unmarshal(options.IncludeUsage, &result.includeUsage) != nil {
				return result, errors.New("stream_options.include_usage must be a boolean")
			}
		}
	}
	if raw, ok := fields["model"]; ok {
		if json.Unmarshal(raw, &result.model) != nil {
			return result, errors.New("model must be a string")
		}
	}
	if _, ok := fields["messages"]; ok {
		if strings.TrimSpace(result.model) == "" {
			return result, errors.New("model is required and must be a nonempty string")
		}
		converted, err := convertChatFields(fields)
		if err != nil {
			return result, err
		}
		fields = converted
	} else if _, ok := fields["input"]; !ok {
		return result, errors.New("messages or input is required")
	}
	delete(fields, "metadata")
	delete(fields, "stream_options")
	fields["stream"] = json.RawMessage("true")
	fields["store"] = json.RawMessage("false")
	var err error
	result.body, err = json.Marshal(fields)
	return result, err
}

func convertChatFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	output := make(map[string]json.RawMessage)
	var text struct {
		Format    *adapterTextFormat `json:"format,omitempty"`
		Verbosity json.RawMessage    `json:"verbosity,omitempty"`
	}
	// Sorting makes validation errors stable when several parameters are unsupported.
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := fields[name]
		switch name {
		case "messages":
			input, instructions, err := convertChatMessages(raw)
			if err != nil {
				return nil, err
			}
			output["input"], _ = json.Marshal(input)
			output["instructions"], _ = json.Marshal(instructions)
		case "model", "parallel_tool_calls", "prompt_cache_key", "service_tier", "safety_identifier":
			output[name] = raw
		case "tools":
			tools, err := convertChatTools(raw)
			if err != nil {
				return nil, err
			}
			output[name], _ = json.Marshal(tools)
		case "tool_choice":
			choice, err := convertChatToolChoice(raw)
			if err != nil {
				return nil, err
			}
			output[name] = choice
		case "reasoning_effort":
			var effort string
			if json.Unmarshal(raw, &effort) != nil || effort == "" {
				return nil, errors.New("reasoning_effort must be a nonempty string")
			}
			output["reasoning"], _ = json.Marshal(struct {
				Effort string `json:"effort"`
			}{effort})
		case "response_format":
			format, err := convertChatFormat(raw)
			if err != nil {
				return nil, err
			}
			text.Format = &format
		case "verbosity":
			text.Verbosity = raw
		case "temperature", "top_p", "max_tokens", "max_completion_tokens", "presence_penalty", "frequency_penalty", "seed", "user", "metadata", "store", "stream_options", "stream":
		case "n", "logprobs", "top_logprobs", "logit_bias", "audio", "modalities":
			if err := validateChatOption(name, raw); err != nil {
				return nil, err
			}
		case "functions", "function_call", "prediction", "web_search_options":
			return nil, fmt.Errorf("unsupported parameter: %s", name)
		default:
			return nil, fmt.Errorf("unsupported parameter: %s", name)
		}
	}
	if text.Format != nil || text.Verbosity != nil {
		output["text"], _ = json.Marshal(text)
	}
	return output, nil
}

func validateChatOption(name string, raw json.RawMessage) error {
	supported := false
	switch name {
	case "n", "top_logprobs":
		var value int
		if json.Unmarshal(raw, &value) == nil {
			supported = (name == "n" && value <= 1) || (name == "top_logprobs" && value <= 0)
		}
	case "logprobs":
		var value bool
		supported = json.Unmarshal(raw, &value) == nil && !value
	case "logit_bias":
		var value map[string]json.RawMessage
		supported = json.Unmarshal(raw, &value) == nil && len(value) == 0
	case "audio":
		supported = bytes.Equal(raw, []byte("null"))
	case "modalities":
		var values []string
		if json.Unmarshal(raw, &values) == nil {
			supported = true
			for _, value := range values {
				if value != "text" {
					supported = false
				}
			}
		}
	}
	if !supported {
		return fmt.Errorf("unsupported parameter: %s", name)
	}
	return nil
}

func convertChatTools(raw json.RawMessage) ([]adapterFunction, error) {
	var tools []struct {
		Type     string          `json:"type"`
		Function adapterFunction `json:"function"`
	}
	if json.Unmarshal(raw, &tools) != nil {
		return nil, errors.New("tools must be an array")
	}
	result := make([]adapterFunction, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			return nil, errors.New("tools require type function and function.name")
		}
		tool.Function.Type = "function"
		result = append(result, tool.Function)
	}
	return result, nil
}

func convertChatToolChoice(raw json.RawMessage) (json.RawMessage, error) {
	var choice string
	if json.Unmarshal(raw, &choice) == nil && (choice == "auto" || choice == "none" || choice == "required") {
		return raw, nil
	}
	var named struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &named) != nil || named.Type != "function" || named.Function.Name == "" {
		return nil, errors.New("unsupported tool_choice")
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}{"function", named.Function.Name})
}

func convertChatFormat(raw json.RawMessage) (adapterTextFormat, error) {
	var format struct {
		Type       string            `json:"type"`
		JSONSchema adapterTextFormat `json:"json_schema"`
	}
	if json.Unmarshal(raw, &format) != nil {
		return adapterTextFormat{}, errors.New("invalid response_format")
	}
	switch format.Type {
	case "text", "json_object":
		return adapterTextFormat{Type: format.Type}, nil
	case "json_schema":
		if format.JSONSchema.Name == "" || len(format.JSONSchema.Schema) == 0 {
			return adapterTextFormat{}, errors.New("json_schema requires name and schema")
		}
		format.JSONSchema.Type = "json_schema"
		return format.JSONSchema, nil
	default:
		return adapterTextFormat{}, errors.New("unsupported response_format")
	}
}
