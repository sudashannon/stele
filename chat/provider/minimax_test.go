package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func miniMaxTestServer(t *testing.T, frames []string, inspect func(map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if inspect != nil {
			inspect(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
}

func collectMiniMaxEvents(t *testing.T, server *httptest.Server, model string, opts ChatOptions) []StreamEvent {
	t.Helper()
	stream, err := (&miniMaxProvider{}).ChatStream(
		context.Background(), "key", server.URL, model, "system",
		[]Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "input"}}}}, opts,
	)
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for event := range stream {
		events = append(events, event)
	}
	return events
}

func TestMiniMaxStreamReportsMaxTokensAndUsesDefaultModel(t *testing.T) {
	server := miniMaxTestServer(t, []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"{\"label\":"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`,
	}, func(body map[string]any) {
		if body["model"] != "MiniMax-M3" {
			t.Fatalf("model=%v, want MiniMax-M3", body["model"])
		}
		thinking, ok := body["thinking"].(map[string]any)
		if !ok || thinking["type"] != "disabled" {
			t.Fatalf("thinking=%v, want disabled", body["thinking"])
		}
	})
	defer server.Close()

	events := collectMiniMaxEvents(t, server, "", ChatOptions{Thinking: "disabled", MaxTokens: 4096})
	if len(events) != 2 || events[0].Type != "delta" || events[1].Type != "error" || !strings.Contains(events[1].Error, "max_tokens") {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestMiniMaxStreamRejectsPrematureEOF(t *testing.T) {
	server := miniMaxTestServer(t, []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`,
	}, nil)
	defer server.Close()

	events := collectMiniMaxEvents(t, server, "MiniMax-M3", ChatOptions{})
	if len(events) != 2 || events[1].Type != "error" || !strings.Contains(events[1].Error, "before message_stop") {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestMiniMaxStreamEmitsDoneAfterNaturalStop(t *testing.T) {
	server := miniMaxTestServer(t, []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`{"type":"message_stop"}`,
	}, nil)
	defer server.Close()

	events := collectMiniMaxEvents(t, server, "MiniMax-M3", ChatOptions{})
	if len(events) != 2 || events[0] != (StreamEvent{Type: "delta", Content: "ok"}) || events[1] != (StreamEvent{Type: "done"}) {
		t.Fatalf("unexpected events: %+v", events)
	}
}
