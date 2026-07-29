package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register(&miniMaxProvider{})
}

type miniMaxProvider struct{}

func (p *miniMaxProvider) Name() string { return "minimax" }

func (p *miniMaxProvider) Models() []string {
	return []string{
		"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed",
		"MiniMax-M2.5", "MiniMax-M2.5-highspeed",
		"MiniMax-M2.1", "MiniMax-M2.1-highspeed", "MiniMax-M2",
	}
}

func (p *miniMaxProvider) SupportsImages() bool { return true }

func (p *miniMaxProvider) ChatStream(ctx context.Context, apiKey, apiBase, model,
	systemPrompt string, messages []Message, opts ChatOptions) (<-chan StreamEvent, error) {

	type reqBody struct {
		Model       string    `json:"model"`
		MaxTokens   int       `json:"max_tokens"`
		System      string    `json:"system"`
		Messages    []Message `json:"messages"`
		Stream      bool      `json:"stream"`
		Temperature float64   `json:"temperature,omitempty"`
		Thinking    any       `json:"thinking,omitempty"`
	}

	if strings.TrimSpace(model) == "" {
		model = p.Models()[0]
	}

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	temp := opts.Temperature
	if temp == 0 {
		temp = 1.0
	}

	var thinking any
	if opts.Thinking == "disabled" {
		thinking = map[string]string{"type": "disabled"}
	}

	body := reqBody{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      systemPrompt,
		Messages:    messages,
		Stream:      true,
		Temperature: temp,
		Thinking:    thinking,
	}

	jsonBody, _ := json.Marshal(body)
	url := strings.TrimRight(apiBase, "/") + "/anthropic/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		var errBody struct{ Error struct{ Message string } }
		json.NewDecoder(resp.Body).Decode(&errBody)
		resp.Body.Close()
		return nil, fmt.Errorf("minimax error %d: %s", resp.StatusCode, errBody.Error.Message)
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		sawMessageStop := false
		send := func(event StreamEvent) bool {
			select {
			case ch <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		sendError := func(err error) {
			if err != nil {
				send(StreamEvent{Type: "error", Error: err.Error()})
			}
		}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					Thinking   string `json:"thinking"`
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				sendError(fmt.Errorf("invalid minimax stream event: %w", err))
				return
			}

			switch event.Type {
			case "content_block_delta":
				switch event.Delta.Type {
				case "thinking_delta":
					if !send(StreamEvent{Type: "thinking", Content: event.Delta.Thinking}) {
						return
					}
				case "text_delta":
					if !send(StreamEvent{Type: "delta", Content: event.Delta.Text}) {
						return
					}
				}
			case "message_delta":
				if event.Delta.StopReason != "" && event.Delta.StopReason != "end_turn" {
					sendError(fmt.Errorf("minimax stopped generation: %s", event.Delta.StopReason))
					return
				}
			case "error":
				if event.Error.Message == "" {
					event.Error.Message = "unknown stream error"
				}
				sendError(fmt.Errorf("minimax stream error: %s", event.Error.Message))
				return
			case "message_stop":
				sawMessageStop = true
				if !send(StreamEvent{Type: "done"}) {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			sendError(fmt.Errorf("read minimax stream: %w", err))
			return
		}
		if !sawMessageStop && ctx.Err() == nil {
			sendError(fmt.Errorf("minimax stream ended before message_stop"))
		}
	}()
	return ch, nil
}
