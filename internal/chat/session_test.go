package chat

import (
	"testing"

	"squid-os/internal/config"
)

func TestNewSessionIncludesToolsBootstrap(t *testing.T) {
	session := NewSession(config.SessionConfig{Tools: []string{"read_file"}}, config.Paths{})
	assertMessageOrder(t, session.Doc.Messages, "sys0", "env0", "config0", "tools0")

	message := messageByID(session.Doc.Messages, "tools0")
	if message == nil || message.Label != "Tools Enabled" || message.Params["tools"] == "" {
		t.Fatalf("invalid tools bootstrap: %+v", message)
	}
	if message.InputTokens <= 0 {
		t.Fatalf("tools schema tokens should be counted: %+v", message)
	}
}

func TestSetPendingInferenceBeforeFirstTurnRewritesBootstrap(t *testing.T) {
	initial := config.InferenceConfig{
		Provider: "vllm",
		Model:    "qwen",
		Thinking: config.ThinkingConfig{Enabled: false},
	}
	next := config.InferenceConfig{
		Provider: "openai",
		Model:    "gpt",
		Thinking: config.ThinkingConfig{Enabled: true},
	}
	session := NewSession(config.SessionConfig{Inference: initial}, config.Paths{})

	session.SetPendingInference(next)

	if session.Doc.Initial.Inference != next {
		t.Fatalf("initial inference not rewritten: got %+v, want %+v", session.Doc.Initial.Inference, next)
	}
	if session.CurrentInference() != next {
		t.Fatalf("current inference not rewritten: got %+v, want %+v", session.CurrentInference(), next)
	}
	if session.Doc.Pending != nil && session.Doc.Pending.Inference != nil {
		t.Fatalf("bootstrap inference change must not remain pending: %+v", session.Doc.Pending.Inference)
	}
	message := messageByID(session.Doc.Messages, "config0")
	if message == nil {
		t.Fatal("config0 message missing")
	}
	if message.Params["provider"] != "openai" || message.Params["model"] != "gpt" || message.Params["thinking"] != "on" {
		t.Fatalf("config0 not rewritten: %+v", message)
	}
	for _, message := range session.Doc.Messages {
		if message.Label == "Model Switched" || message.Label == "Thinking Switched" {
			t.Fatalf("bootstrap change created a transition: %+v", message)
		}
	}
}

func TestSetPendingInferenceAfterFirstTurnQueuesTransition(t *testing.T) {
	initial := config.InferenceConfig{Provider: "vllm", Model: "qwen"}
	next := config.InferenceConfig{Provider: "openai", Model: "gpt"}
	session := NewSession(config.SessionConfig{Inference: initial}, config.Paths{})
	session.Append(NewUserMessage("msg_1", "hello", ""))

	session.SetPendingInference(next)

	if session.CurrentInference() != initial {
		t.Fatalf("current inference changed before PrepareTurn: %+v", session.CurrentInference())
	}
	if session.Doc.Initial.Inference != initial {
		t.Fatalf("initial inference changed after first turn: %+v", session.Doc.Initial.Inference)
	}
	if session.Doc.Pending == nil || session.Doc.Pending.Inference == nil || *session.Doc.Pending.Inference != next {
		t.Fatalf("inference transition not queued: %+v", session.Doc.Pending)
	}
}

func assertMessageOrder(t *testing.T, messages []config.Message, ids ...string) {
	t.Helper()
	if len(messages) < len(ids) {
		t.Fatalf("got %d messages, want at least %d", len(messages), len(ids))
	}
	for index, id := range ids {
		if messages[index].ID != id {
			t.Fatalf("message %d: got %q, want %q", index, messages[index].ID, id)
		}
	}
}

func messageByID(messages []config.Message, id string) *config.Message {
	for index := range messages {
		if messages[index].ID == id {
			return &messages[index]
		}
	}
	return nil
}
