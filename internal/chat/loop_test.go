package chat

import "testing"

func TestProcessStreamEventFinalizesToolAtStreamIndex(t *testing.T) {
	s := &Session{}

	tools := []ToolCall{
		{ID: "call_1", Name: "skill_load", ArgsJSON: `{"name":"plan-runner"}`},
		{ID: "call_2", Name: "read_file", ArgsJSON: `{"path":"one"}`},
		{ID: "call_3", Name: "read_file", ArgsJSON: `{"path":"two"}`},
	}

	for idx, tc := range tools {
		ProcessStreamEvent(s, StreamEvent{
			ToolCallDelta: tc.ArgsJSON,
			ToolCallIdx:   idx,
			ToolCallName:  tc.Name,
			ToolCallID:    tc.ID,
		})
		ProcessStreamEvent(s, StreamEvent{
			ToolCalls:   []ToolCall{tc},
			ToolCallIdx: idx,
		})
	}

	if len(s.Stream.PartialTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(s.Stream.PartialTools))
	}
	for i, want := range tools {
		got := s.Stream.PartialTools[i]
		if got.ID != want.ID || got.Name != want.Name || got.Args != want.ArgsJSON {
			t.Fatalf("tool %d mismatch: got %#v, want %#v", i, got, want)
		}
	}
}
