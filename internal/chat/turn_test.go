package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/skills"
)

func TestPrepareTurnCommitsPendingInference(t *testing.T) {
	cfg := config.SessionConfig{Inference: config.InferenceConfig{Provider: "vllm", Model: "qwen"}}
	s := &Session{Doc: config.NewSessionDoc(cfg)}
	next := config.InferenceConfig{Provider: "openai", Model: "gpt"}
	s.SetPendingInference(next)
	s.Append(NewUserMessage("u", "hello", ""))
	if err := PrepareTurn(s); err != nil {
		t.Fatal(err)
	}
	if s.CurrentInference() != next {
		t.Fatalf("pending inference not committed: %+v", s.CurrentInference())
	}
	if s.Doc.Pending != nil && s.Doc.Pending.Inference != nil {
		t.Fatalf("pending inference not cleared: %+v", s.Doc.Pending.Inference)
	}
	if len(s.Doc.Messages) != 2 || s.Doc.Messages[1].Label != "Model Switched" {
		t.Fatalf("switch message missing: %+v", s.Doc.Messages)
	}
}

func TestSyntheticMessagesUseInputTokensOnly(t *testing.T) {
	session := &Session{}
	index := AppendStreamErrorMessage(session, fmt.Errorf("boom"))
	message := session.Doc.Messages[index]
	if message.InputTokens <= 0 {
		t.Fatalf("synthetic input tokens missing: %+v", message)
	}
	if message.TextMetrics.Tokens != 0 || message.OutputTokens != 0 {
		t.Fatalf("synthetic message must not contain output tokens: %+v", message)
	}
}

func TestPrepareTurnTransitionPlacement(t *testing.T) {
	cfg := config.SessionConfig{Inference: config.InferenceConfig{Provider: "p", Model: "a"}, Tools: []string{"read"}}
	s := &Session{Doc: config.NewSessionDoc(cfg)}
	s.Append(NewUserMessage("u", "hello", ""))
	s.SetPendingInference(config.InferenceConfig{Provider: "p", Model: "b"})
	s.Doc.Pending.Tools = &[]string{"bash"}
	err := PrepareTurn(s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Doc.Messages[0].Role != config.RoleUser || len(s.Doc.Messages) < 2 {
		t.Fatalf("unexpected messages: %+v", s.Doc.Messages)
	}
	if s.CurrentInference().Model != "b" {
		t.Fatal("inference not updated")
	}
	if s.Doc.Messages[1].Role != config.RoleInternal || s.Doc.Messages[1].Label != "Model Switched" {
		t.Fatalf("model transition should remain internal: %+v", s.Doc.Messages)
	}
}

func TestPrepareTurnSkillChangeIncludesBodyIDAndTokens(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: review\ndescription: Reviews code\n---\nReview carefully and report defects.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := skills.InitRegistry(root); err != nil {
		t.Fatal(err)
	}

	cfg := config.SessionConfig{Inference: config.InferenceConfig{Provider: "p", Model: "m"}}
	s := &Session{Doc: config.NewSessionDoc(cfg)}
	s.Append(NewUserMessage("msg_1", "hello", ""))
	s.SetPendingSkill("review")
	if err := PrepareTurn(s); err != nil {
		t.Fatal(err)
	}

	msg := s.Doc.Messages[1]
	if msg.ID != "msg_2" {
		t.Fatalf("missing synthetic message ID: %+v", msg)
	}
	if !strings.Contains(msg.Text, "Review carefully and report defects.") {
		t.Fatalf("skill body missing: %q", msg.Text)
	}
	if msg.InputTokens != CountTokensApproxString(msg.Text) || msg.InputTokens <= 0 {
		t.Fatalf("bad input tokens: %+v", msg)
	}
	if s.TotalInputTokens() != s.Doc.Messages[0].InputTokens+msg.InputTokens {
		t.Fatalf("session input tokens not accumulated: %d", s.TotalInputTokens())
	}
}

func TestPrepareTurnSkillChangeIsSynthetic(t *testing.T) {
	cfg := config.SessionConfig{Inference: config.InferenceConfig{Provider: "p", Model: "m"}}
	s := &Session{Doc: config.NewSessionDoc(cfg)}
	s.Append(NewUserMessage("u", "hello", ""))
	s.SetPendingSkill("review")
	if err := PrepareTurn(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Doc.Messages) != 2 {
		t.Fatalf("messages: %+v", s.Doc.Messages)
	}
	msg := s.Doc.Messages[1]
	if msg.Role != config.RoleSynthetic || msg.Label != "skill_load" || msg.Params["name"] != "review" {
		t.Fatalf("skill change should be synthetic: %+v", msg)
	}
	apiMessages := s.BuildMessages()
	if len(apiMessages) != 2 {
		t.Fatalf("synthetic skill message should be included in API messages: %+v", apiMessages)
	}
}
