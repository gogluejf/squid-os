package cli

import "testing"

func TestGNUCommandParsesPrompt(t *testing.T) {
	var got *GNUOptions
	command := testRoot(t, nil, nil, &got)
	command.SetArgs([]string{"gnu", "--prompt", "list files", "--print", "--model", "vllm/model"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "list files" || !got.PrintOnly || got.Model != "vllm/model" {
		t.Fatalf("%+v", got)
	}
}

func TestGNUCommandAcceptsPositionalRequest(t *testing.T) {
	var got *GNUOptions
	command := testRoot(t, nil, nil, &got)
	command.SetArgs([]string{"gnu", "list", "large", "files"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "list large files" {
		t.Fatalf("%+v", got)
	}
}

func TestGNUCommandRejectsTwoPromptSources(t *testing.T) {
	command := testRoot(t, nil, nil, nil)
	command.SetArgs([]string{"gnu", "positional", "--prompt", "flag"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected conflict")
	}
}
