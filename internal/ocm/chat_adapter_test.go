package ocm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func assertChatJSON(t *testing.T, actual []byte, expected string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON mismatch:\ngot  %s\nwant %s", actual, expected)
	}
}

func TestChatAdapterRequest(t *testing.T) {
	cases := []struct{ name, input, output string }{
		{"conversation", `{"model":"gpt-5.4","messages":[{"role":"system","content":"first"},{"role":"developer","content":[{"type":"text","text":"second"}]},{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"text","text":"answer"},{"type":"refusal","refusal":"no"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"}]}`,
			`{"model":"gpt-5.4","stream":true,"store":false,"instructions":"first\n\nsecond","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answerno"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"result"}]}`},
		{"parts", `{"model":"gpt-5.4","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.org/image"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA","detail":"low"}},{"type":"file","file":{"file_id":"file_1","file_data":"data:abc","filename":"a.txt"}}]}]}`,
			`{"model":"gpt-5.4","stream":true,"store":false,"instructions":"","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"https://example.org/image","detail":"auto"},{"type":"input_image","image_url":"data:image/png;base64,AA","detail":"low"},{"type":"input_file","file_id":"file_1","file_data":"data:abc","filename":"a.txt"}]}]}`},
		{"tools and text", `{"model":"gpt-5.4","messages":[],"tools":[{"type":"function","function":{"name":"lookup","description":"desc","parameters":{"type":"object"},"strict":false}},{"type":"function","function":{"name":"minimal"}}],"tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false,"reasoning_effort":"high","verbosity":"low","response_format":{"type":"json_schema","json_schema":{"name":"answer","description":"desc","schema":{"type":"object"},"strict":true}},"prompt_cache_key":"cache","service_tier":"default","safety_identifier":"safe"}`,
			`{"model":"gpt-5.4","stream":true,"store":false,"instructions":"","input":[],"tools":[{"type":"function","name":"lookup","description":"desc","parameters":{"type":"object"},"strict":false},{"type":"function","name":"minimal"}],"tool_choice":{"type":"function","name":"lookup"},"parallel_tool_calls":false,"reasoning":{"effort":"high"},"text":{"verbosity":"low","format":{"type":"json_schema","name":"answer","description":"desc","schema":{"type":"object"},"strict":true}},"prompt_cache_key":"cache","service_tier":"default","safety_identifier":"safe"}`},
		{"responses passthrough", `{"model":"gpt-5.4","input":"hi","future_field":{"x":1},"metadata":{"x":"y"},"stream_options":{"include_usage":true},"store":true,"stream":false}`, `{"model":"gpt-5.4","input":"hi","future_field":{"x":1},"stream":true,"store":false}`},
		{"ignored", `{"model":"gpt-5.4","messages":[],"temperature":1,"top_p":1,"max_tokens":1,"max_completion_tokens":1,"presence_penalty":1,"frequency_penalty":1,"seed":1,"user":"u","metadata":{},"store":true,"logit_bias":{},"n":1,"logprobs":false,"top_logprobs":0,"modalities":["text"],"audio":null}`, `{"model":"gpt-5.4","instructions":"","input":[],"stream":true,"store":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			converted, err := convertChatRequest([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			assertChatJSON(t, converted.body, tc.output)
			if converted.stream {
				t.Fatal("client streaming default must be false")
			}
			if tc.name == "responses passthrough" && !converted.includeUsage {
				t.Fatal("include_usage lost")
			}
		})
	}
	for _, format := range []string{"text", "json_object"} {
		converted, err := convertChatRequest([]byte(`{"model":"m","messages":[],"response_format":{"type":"` + format + `"}}`))
		if err != nil || !bytes.Contains(converted.body, []byte(`"format":{"type":"`+format+`"}`)) {
			t.Fatalf("format %s: %s %v", format, converted.body, err)
		}
	}
	for _, choice := range []string{"auto", "none", "required"} {
		if _, err := convertChatRequest([]byte(`{"model":"m","messages":[],"tool_choice":"` + choice + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatAdapterRequestErrors(t *testing.T) {
	for _, body := range []string{
		`{`, `null`, `[]`, `{}`, `{"messages":[]}`, `{"model":" ","messages":[]}`,
		`{"model":5,"messages":[]}`, `{"model":"m","messages":null}`,
		`{"model":"m","messages":[{"role":"function","content":"x"}]}`,
		`{"model":"m","messages":[{"role":"new_role"}]}`,
		`{"model":"m","messages":[{"role":"user","content":[{"type":"audio"}]}]}`,
		`{"model":"m","messages":[{"role":"tool","content":"x"}]}`,
		`{"model":"m","messages":[{"role":"assistant","function_call":{}}]}`,
		`{"model":"m","messages":[],"n":2}`, `{"model":"m","messages":[],"logprobs":true}`,
		`{"model":"m","messages":[],"top_logprobs":1}`, `{"model":"m","messages":[],"logit_bias":{"1":1}}`,
		`{"model":"m","messages":[],"functions":[]}`, `{"model":"m","messages":[],"function_call":"auto"}`,
		`{"model":"m","messages":[],"prediction":{}}`, `{"model":"m","messages":[],"web_search_options":{}}`,
		`{"model":"m","messages":[],"modalities":["audio"]}`, `{"model":"m","messages":[],"audio":{}}`,
		`{"model":"m","messages":[],"tools":[{"type":"web_search"}]}`,
		`{"model":"m","messages":[],"tool_choice":"invalid"}`,
		`{"model":"m","messages":[],"response_format":{"type":"invalid"}}`,
		`{"model":"m","messages":[],"stream":"true"}`,
		`{"model":"m","messages":[],"stream_options":{"include_usage":"true"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := convertChatRequest([]byte(body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := convertChatRequest([]byte(`{"model":"m","messages":[],"unknown":1}`)); err == nil || err.Error() != "unsupported parameter: unknown" {
		t.Fatalf("unknown parameter: %v", err)
	}
}

const adapterCreated = `{"type":"response.created","response":{"id":"resp_test","model":"gpt-5.4","created_at":123}}`
const adapterTextDelta = `{"type":"response.output_text.delta","delta":"Hello"}`
const adapterToolAdded = `{"type":"response.output_item.added","output_index":3,"item":{"type":"function_call","call_id":"call_1","name":"lookup"}}`
const adapterToolArguments = `{"type":"response.function_call_arguments.delta","output_index":3,"delta":"{\"x\":1}"}`

func chatSSE(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

func adapterRequest(t *testing.T, service *Service, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?test=1", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer client-key")
	return request
}

func chatChunks(t *testing.T, body string) []chatChunk {
	t.Helper()
	var chunks []chatChunk
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func TestChatAdapterIntegration(t *testing.T) {
	for _, stream := range []bool{true, false} {
		for _, compressed := range []bool{true, false} {
			name := "json"
			if stream {
				name = "stream"
			}
			if compressed {
				name += " gzip"
			}
			t.Run(name, func(t *testing.T) {
				final := strings.Replace(completedEvent, `"output_tokens":20`, `"output_tokens":20,"total_tokens":120,"output_tokens_details":{"reasoning_tokens":7}`, 1)
				data := chatSSE(adapterCreated, adapterTextDelta, adapterToolAdded, adapterToolArguments, final, final)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "POST" || r.URL.Path != "/responses" || r.URL.RawQuery != "test=1" || r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Originator") != codexOriginator || r.Header.Get("ChatGPT-Account-Id") != "account-id" {
						t.Errorf("unexpected upstream request: %s %v", r.URL, r.Header)
					}
					body, _ := io.ReadAll(r.Body)
					if bytes.Contains(body, []byte("client-key")) || strings.Contains(r.Header.Get("Authorization"), "client-key") {
						t.Error("client token leaked")
					}
					assertChatJSON(t, body, `{"model":"gpt-5.4","instructions":"system","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"stream":true,"store":false}`)
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Request-Id", "test-id")
					if compressed {
						w.Header().Set("Content-Encoding", "gzip")
						writer := gzip.NewWriter(w)
						_, _ = io.WriteString(writer, data)
						_ = writer.Close()
						return
					}
					for len(data) > 0 {
						count := min(17, len(data))
						_, _ = io.WriteString(w, data[:count])
						w.(http.Flusher).Flush()
						data = data[count:]
					}
				}))
				defer upstream.Close()
				service := startTestService(t, upstream.URL, true, io.Discard)
				streamValue := "false"
				if stream {
					streamValue = "true"
				}
				request := adapterRequest(t, service, `{"model":"gpt-5.4","messages":[{"role":"system","content":"system"},{"role":"user","content":"hi"}],"stream":`+streamValue+`,"stream_options":{"include_usage":true}}`)
				writer := httptest.NewRecorder()
				service.ServeHTTP(writer, request)
				if writer.Code != 200 || writer.Header().Get("X-Request-Id") != "test-id" || writer.Header().Get("Content-Encoding") != "" || writer.Header().Get("Content-Length") != "" {
					t.Fatalf("response: %d %v %s", writer.Code, writer.Header(), writer.Body)
				}
				if stream {
					if writer.Header().Get("Content-Type") != "text/event-stream" || writer.Header().Get("Cache-Control") != "no-cache" || !strings.HasSuffix(writer.Body.String(), "data: [DONE]\n\n") {
						t.Fatalf("stream headers/end: %v %s", writer.Header(), writer.Body)
					}
					chunks := chatChunks(t, writer.Body.String())
					if len(chunks) != 5 {
						t.Fatalf("chunks: %s", writer.Body)
					}
					for _, chunk := range chunks {
						if chunk.ID != "resp_test" || chunk.Object != "chat.completion.chunk" || chunk.Created != 123 || chunk.Model != "gpt-5.4" {
							t.Fatalf("chunk metadata: %+v", chunk)
						}
					}
					first := chunks[0].Choices[0]
					if first.Delta.Role != "assistant" || first.Delta.Content == nil || *first.Delta.Content != "Hello" || first.FinishReason != nil {
						t.Fatalf("first delta: %+v", first)
					}
					tool := chunks[1].Choices[0].Delta.ToolCalls[0]
					if tool.Index != 0 || tool.ID != "call_1" || tool.Type != "function" || tool.Function.Name != "lookup" || tool.Function.Arguments != "" {
						t.Fatalf("tool: %+v", tool)
					}
					if chunks[2].Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"x":1}` || *chunks[3].Choices[0].FinishReason != "tool_calls" {
						t.Fatalf("tool final: %s", writer.Body)
					}
					usage := chunks[4].Usage
					if len(chunks[4].Choices) != 0 || usage == nil || usage.PromptTokens != 100 || usage.CompletionTokens != 20 || usage.TotalTokens != 120 || usage.PromptTokensDetails.CachedTokens != 10 || usage.CompletionTokensDetails.ReasoningTokens != 7 {
						t.Fatalf("usage: %+v", usage)
					}
				} else {
					assertChatJSON(t, writer.Body.Bytes(), `{"id":"resp_test","object":"chat.completion","created":123,"model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"Hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":10},"completion_tokens_details":{"reasoning_tokens":7}},"service_tier":"default"}`)
				}
				if err := service.Close(); err != nil {
					t.Fatal(err)
				}
				assertSavedUsage(t, testUsagePath(service))
			})
		}
	}
}

func TestChatAdapterResponseOutcomes(t *testing.T) {
	for _, tc := range []struct{ name, event, finish, errorMessage string }{
		{name: "incomplete length", event: `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, finish: "length"},
		{name: "incomplete filter", event: `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`, finish: "content_filter"},
		{name: "stop", event: completedEvent, finish: "stop"},
		{name: "final tool only", event: `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"final_call","name":"lookup","arguments":"{}"}]}}`, finish: "tool_calls"},
		{name: "failed", event: `{"type":"response.failed","response":{"error":{"message":"failure"}}}`, errorMessage: "failure"},
		{name: "error", event: `{"type":"error","message":"failure"}`, errorMessage: "failure"},
		{name: "eof", event: adapterTextDelta, errorMessage: "upstream stream ended unexpectedly"},
		{name: "invalid json", event: `{`, errorMessage: "invalid upstream SSE event"},
	} {
		for _, stream := range []bool{true, false} {
			t.Run(tc.name+map[bool]string{true: " stream", false: " json"}[stream], func(t *testing.T) {
				service := startTestService(t, "http://unused", true, io.Discard)
				response := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(chatSSE(adapterCreated, tc.event)))}
				writer := httptest.NewRecorder()
				service.convertChatResponse(writer, adapterRequest(t, service, ""), response, chatAdapterRequest{model: "gpt-5.4", stream: stream}, "tester")
				if tc.errorMessage != "" {
					if !strings.Contains(writer.Body.String(), tc.errorMessage) || !strings.Contains(writer.Body.String(), `"type":"api_error"`) || strings.Contains(writer.Body.String(), "[DONE]") {
						t.Fatalf("error: %s", writer.Body)
					}
					if !stream && writer.Code != 502 {
						t.Fatalf("status %d", writer.Code)
					}
				} else {
					if writer.Code != 200 || !strings.Contains(writer.Body.String(), `"finish_reason":"`+tc.finish+`"`) {
						t.Fatalf("finish: %d %s", writer.Code, writer.Body)
					}
					if stream && strings.Contains(writer.Body.String(), `"choices":[]`) {
						t.Fatal("unsolicited usage chunk")
					}
					if tc.name == "final tool only" && !stream && !strings.Contains(writer.Body.String(), `"content":null`) {
						t.Fatal("tool-only content must be null")
					}
				}
			})
		}
	}
}

func TestChatAdapterClientWriteFailure(t *testing.T) {
	service := startTestService(t, "http://unused", true, io.Discard)
	response := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(chatSSE(adapterCreated, adapterTextDelta, completedEvent, completedEvent)))}
	service.convertChatResponse(disconnectedWriter{httptest.NewRecorder()}, adapterRequest(t, service, ""), response, chatAdapterRequest{model: "gpt-5.4", stream: true}, "tester")
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	assertSavedUsage(t, testUsagePath(service))
}

func TestChatAdapterHTTPFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "bad-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream bad request"}}`)
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	for _, stream := range []bool{true, false} {
		body := `{"model":"m","messages":[],"stream":false}`
		if stream {
			body = strings.Replace(body, "false", "true", 1)
		}
		writer := httptest.NewRecorder()
		service.ServeHTTP(writer, adapterRequest(t, service, body))
		if writer.Code != 400 || writer.Header().Get("X-Request-Id") != "bad-id" || writer.Body.String() != `{"error":{"message":"upstream bad request"}}` {
			t.Fatalf("passthrough: %d %v %s", writer.Code, writer.Header(), writer.Body)
		}
	}
}

func TestChatAdapterRejectsBeforeUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected upstream request") }))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	for _, tc := range []struct {
		method, encoding, body string
		status                 int
	}{
		{"GET", "", "", 405}, {"POST", "gzip", `{"input":"hi"}`, 400}, {"POST", "", "{", 400}, {"POST", "", `{"model":"m"}`, 400},
	} {
		writer := httptest.NewRecorder()
		request := adapterRequest(t, service, tc.body)
		request.Method = tc.method
		request.Header.Set("Content-Encoding", tc.encoding)
		service.ServeHTTP(writer, request)
		if writer.Code != tc.status || !strings.Contains(writer.Body.String(), `"type":"invalid_request_error"`) {
			t.Fatalf("rejection: %d %s", writer.Code, writer.Body)
		}
	}
}

func TestChatAdapterUnknownTierStillReturnsResponse(t *testing.T) {
	var logs bytes.Buffer
	service := startTestService(t, "http://unused", true, &logs)
	response := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(chatSSE(strings.Replace(completedEvent, `"default"`, `"future-tier"`, 1))))}
	writer := httptest.NewRecorder()
	service.convertChatResponse(writer, adapterRequest(t, service, ""), response, chatAdapterRequest{model: "gpt-5.4"}, "tester")
	if writer.Code != 200 || !strings.Contains(writer.Body.String(), `"service_tier":"future-tier"`) || !strings.Contains(logs.String(), "parse chat adapter usage") {
		t.Fatalf("tier handling: %d %s %s", writer.Code, writer.Body, logs.String())
	}
}

func TestChatAdapterCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-time.After(5 * time.Second):
			t.Error("upstream request context was not canceled")
		}
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := adapterRequest(t, service, `{"model":"m","messages":[]}`).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); service.ServeHTTP(httptest.NewRecorder(), request) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not cancel")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not cancel")
	}
}
