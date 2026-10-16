package ocm

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type chatMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	ToolCalls    []chatToolCall  `json:"tool_calls"`
	ToolCallID   string          `json:"tool_call_id"`
	FunctionCall json.RawMessage `json:"function_call"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatContentPart struct {
	Type     string  `json:"type"`
	Text     *string `json:"text"`
	Refusal  *string `json:"refusal"`
	ImageURL *struct {
		URL    string  `json:"url"`
		Detail *string `json:"detail"`
	} `json:"image_url"`
	File *struct {
		FileData *string `json:"file_data,omitempty"`
		FileID   *string `json:"file_id,omitempty"`
		Filename *string `json:"filename,omitempty"`
	} `json:"file"`
}

type adapterInputPart struct {
	Type     string  `json:"type"`
	Text     *string `json:"text,omitempty"`
	ImageURL string  `json:"image_url,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	FileData *string `json:"file_data,omitempty"`
	FileID   *string `json:"file_id,omitempty"`
	Filename *string `json:"filename,omitempty"`
}

type adapterInput struct {
	Type      string             `json:"type"`
	Role      string             `json:"role,omitempty"`
	Content   []adapterInputPart `json:"content,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments *string            `json:"arguments,omitempty"`
	Output    *string            `json:"output,omitempty"`
}

func convertChatMessages(raw json.RawMessage) ([]adapterInput, string, error) {
	var messages []chatMessage
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &messages) != nil {
		return nil, "", errors.New("messages must be an array")
	}
	input := make([]adapterInput, 0)
	instructions := make([]string, 0)
	for _, message := range messages {
		items, instruction, err := convertChatMessage(message)
		if err != nil {
			return nil, "", err
		}
		input = append(input, items...)
		if message.Role == "system" || message.Role == "developer" {
			instructions = append(instructions, instruction)
		}
	}
	return input, strings.Join(instructions, "\n\n"), nil
}

func convertChatMessage(message chatMessage) ([]adapterInput, string, error) {
	if message.FunctionCall != nil {
		return nil, "", errors.New("assistant function_call is unsupported; use tool_calls")
	}
	parts, err := convertChatContent(message.Content, message.Role)
	if err != nil {
		return nil, "", err
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Text != nil {
			text.WriteString(*part.Text)
		}
	}
	switch message.Role {
	case "system", "developer":
		return nil, text.String(), nil
	case "user":
		return []adapterInput{{Type: "message", Role: "user", Content: parts}}, "", nil
	case "assistant":
		items := make([]adapterInput, 0)
		if text.Len() > 0 {
			content := text.String()
			items = append(items, adapterInput{Type: "message", Role: "assistant", Content: []adapterInputPart{{Type: "output_text", Text: &content}}})
		}
		for _, call := range message.ToolCalls {
			if call.Type != "function" || call.ID == "" || call.Function.Name == "" {
				return nil, "", errors.New("tool_calls require type function, id and function.name")
			}
			items = append(items, adapterInput{Type: "function_call", CallID: call.ID, Name: call.Function.Name, Arguments: &call.Function.Arguments})
		}
		return items, "", nil
	case "tool":
		if message.ToolCallID == "" {
			return nil, "", errors.New("tool_call_id is required")
		}
		output := text.String()
		return []adapterInput{{Type: "function_call_output", CallID: message.ToolCallID, Output: &output}}, "", nil
	default:
		return nil, "", errors.New("unsupported message role")
	}
}

func convertChatContent(raw json.RawMessage, role string) ([]adapterInputPart, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		if role != "assistant" {
			return nil, errors.New("message content must be a string or an array")
		}
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []adapterInputPart{{Type: "input_text", Text: &text}}, nil
	}
	var parts []chatContentPart
	if json.Unmarshal(raw, &parts) != nil {
		return nil, errors.New("message content must be a string or an array")
	}
	result := make([]adapterInputPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.Text == nil {
				return nil, errors.New("text content requires text")
			}
			result = append(result, adapterInputPart{Type: "input_text", Text: part.Text})
		case "refusal":
			if role != "assistant" || part.Refusal == nil {
				return nil, errors.New("refusal content requires assistant role and refusal text")
			}
			result = append(result, adapterInputPart{Type: "output_text", Text: part.Refusal})
		case "image_url":
			if role != "user" || part.ImageURL == nil || part.ImageURL.URL == "" {
				return nil, errors.New("image_url requires user role and url")
			}
			detail := "auto"
			if part.ImageURL.Detail != nil {
				detail = *part.ImageURL.Detail
			}
			result = append(result, adapterInputPart{Type: "input_image", ImageURL: part.ImageURL.URL, Detail: detail})
		case "file":
			if role != "user" || part.File == nil {
				return nil, errors.New("file content requires user role and file")
			}
			result = append(result, adapterInputPart{Type: "input_file", FileData: part.File.FileData, FileID: part.File.FileID, Filename: part.File.Filename})
		default:
			return nil, errors.New("unsupported message content type")
		}
	}
	return result, nil
}
