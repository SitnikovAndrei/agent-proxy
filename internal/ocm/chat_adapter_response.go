package ocm

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/openai/openai-go/v3/responses"
)

type adapterEvent struct {
	Type        string             `json:"type"`
	Response    responses.Response `json:"response"`
	Delta       string             `json:"delta"`
	OutputIndex int                `json:"output_index"`
	Item        struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
	} `json:"item"`
	Message string `json:"message"`
}

type chatDeltaFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type chatDeltaTool struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function chatDeltaFunction `json:"function"`
}

type chatDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	ToolCalls []chatDeltaTool `json:"tool_calls,omitempty"`
}

type chatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type chatAdapterUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type chatChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []chatChunkChoice `json:"choices"`
	Usage   *chatAdapterUsage `json:"usage,omitempty"`
}

type chatAdapterResponse struct {
	id           string
	model        string
	created      int64
	text         strings.Builder
	tools        []chatToolCall
	toolIndices  map[int]int
	roleSent     bool
	final        *responses.Response
	finishReason string
	stream       bool
}

func (c *chatAdapterResponse) identify(response responses.Response) {
	if response.ID != "" {
		c.id = response.ID
	}
	if response.Model != "" {
		c.model = string(response.Model)
	}
	if response.CreatedAt != 0 {
		c.created = int64(response.CreatedAt)
	}
}

func (c *chatAdapterResponse) consume(event adapterEvent) (*chatDelta, error) {
	if c.final != nil {
		return nil, nil
	}
	var delta chatDelta
	switch event.Type {
	case "response.created", "response.in_progress":
		c.identify(event.Response)
	case "response.output_text.delta":
		delta.Content = &event.Delta
		if !c.stream {
			c.text.WriteString(event.Delta)
		}
	case "response.output_item.added":
		if event.Item.Type != "function_call" {
			return nil, nil
		}
		if _, exists := c.toolIndices[event.OutputIndex]; exists {
			return nil, errors.New("duplicate upstream function call index")
		}
		index := len(c.tools)
		c.toolIndices[event.OutputIndex] = index
		c.tools = append(c.tools, chatToolCall{ID: event.Item.CallID, Type: "function", Function: chatFunction{Name: event.Item.Name}})
		delta.ToolCalls = []chatDeltaTool{{Index: index, ID: event.Item.CallID, Type: "function", Function: chatDeltaFunction{Name: event.Item.Name}}}
	case "response.function_call_arguments.delta":
		index, exists := c.toolIndices[event.OutputIndex]
		if !exists {
			return nil, errors.New("upstream function arguments have no matching call")
		}
		if !c.stream {
			c.tools[index].Function.Arguments += event.Delta
		}
		delta.ToolCalls = []chatDeltaTool{{Index: index, Function: chatDeltaFunction{Arguments: event.Delta}}}
	case "response.completed", "response.incomplete":
		c.identify(event.Response)
		c.final = &event.Response
		c.finishReason = c.finalReason(event)
	case "response.failed":
		message := event.Response.Error.Message
		if message == "" {
			message = "upstream response failed"
		}
		return nil, errors.New(message)
	case "error":
		if event.Message == "" {
			event.Message = "upstream response failed"
		}
		return nil, errors.New(event.Message)
	}
	if delta.Content == nil && len(delta.ToolCalls) == 0 {
		return nil, nil
	}
	if !c.roleSent {
		delta.Role = "assistant"
		c.roleSent = true
	}
	return &delta, nil
}

func (c *chatAdapterResponse) finalReason(event adapterEvent) string {
	hasTools := len(c.tools) > 0
	for _, item := range event.Response.Output {
		if item.Type == "function_call" {
			hasTools = true
		}
	}
	if hasTools {
		return "tool_calls"
	}
	if event.Type == "response.incomplete" {
		switch event.Response.IncompleteDetails.Reason {
		case "max_output_tokens":
			return "length"
		case "content_filter":
			return "content_filter"
		}
	}
	return "stop"
}

func (c *chatAdapterResponse) chunk(delta chatDelta, finish *string) chatChunk {
	return chatChunk{ID: c.id, Object: "chat.completion.chunk", Created: c.created, Model: c.model,
		Choices: []chatChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}}}
}

func (c *chatAdapterResponse) usage() chatAdapterUsage {
	usage := c.final.Usage
	result := chatAdapterUsage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}
	result.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
	result.CompletionTokensDetails.ReasoningTokens = usage.OutputTokensDetails.ReasoningTokens
	return result
}

type chatCompletionMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      chatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type adapterChatCompletion struct {
	ID          string                 `json:"id"`
	Object      string                 `json:"object"`
	Created     int64                  `json:"created"`
	Model       string                 `json:"model"`
	Choices     []chatCompletionChoice `json:"choices"`
	Usage       chatAdapterUsage       `json:"usage"`
	ServiceTier string                 `json:"service_tier,omitempty"`
}

func (c *chatAdapterResponse) completion() adapterChatCompletion {
	// The final output also covers providers which omit incremental item events.
	for _, item := range c.final.Output {
		if item.Type != "function_call" {
			continue
		}
		call := chatToolCall{ID: item.CallID, Type: "function", Function: chatFunction{Name: item.Name, Arguments: item.AsFunctionCall().Arguments}}
		found := false
		for index := range c.tools {
			if c.tools[index].ID == item.CallID {
				c.tools[index] = call
				found = true
				break
			}
		}
		if !found {
			c.tools = append(c.tools, call)
		}
	}
	text := c.text.String()
	if text == "" {
		text = c.final.OutputText()
	}
	var content *string
	if text != "" || len(c.tools) == 0 {
		content = &text
	}
	return adapterChatCompletion{ID: c.id, Object: "chat.completion", Created: c.created, Model: c.model,
		Choices: []chatCompletionChoice{{Message: chatCompletionMessage{Role: "assistant", Content: content, ToolCalls: c.tools}, FinishReason: c.finishReason}},
		Usage:   c.usage(), ServiceTier: string(c.final.ServiceTier)}
}

func readChatEvents(body io.Reader, consume func(adapterEvent) error) error {
	reader := bufio.NewReader(body)
	for {
		line, readErr := reader.ReadString('\n')
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			data = strings.TrimSpace(data)
			if data != "[DONE]" {
				var event adapterEvent
				if json.Unmarshal([]byte(data), &event) != nil {
					return errors.New("invalid upstream SSE event")
				}
				if err := consume(event); err != nil {
					if errors.Is(err, io.EOF) {
						return nil
					}
					return err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return errors.New("cannot read upstream stream")
		}
	}
}
