package run

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOutputModes(t *testing.T) {
	var out, errout bytes.Buffer
	r := Result{FinalText: "answer", SavedSessionName: "saved"}
	if err := WriteResult(OutputFinalMessage, r, &out, &errout); err != nil {
		t.Fatal(err)
	}
	if out.String() != "answer\n" || errout.String() != "saved\n" {
		t.Fatalf("%q %q", out.String(), errout.String())
	}
	out.Reset()
	errout.Reset()
	if err := WriteResult(OutputSilent, r, &out, &errout); err != nil || out.Len() != 0 {
		t.Fatal("silent output failed")
	}
}
func TestStreamEnvelope(t *testing.T) {
	var out bytes.Buffer
	saved := true
	w := NewStreamWriter(&out)
	if err := w.Write(StreamEnvelope{Event: "session_start", Saved: &saved, SessionName: "s"}); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &value); err != nil {
		t.Fatal(err)
	}
	if value["event"] != "session_start" || value["session"] != "s" {
		t.Fatalf("%v", value)
	}
}
