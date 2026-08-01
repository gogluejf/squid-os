package gnu

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildPromptIncludesPlatformAndSafetyContract(t *testing.T) {
	prompt := BuildPrompt("list large files", Platform{Description: "macOS with BSD userland", PackageManager: "brew install", Shell: "/bin/zsh", Home: "/Users/a", User: "a", WorkingDir: "/tmp"})
	for _, value := range []string{"macOS with BSD userland", "brew install", "Never use sudo", "list large files", `{"command":"...","install_hint":"..."}`} {
		if !strings.Contains(prompt, value) {
			t.Fatalf("prompt missing %q", value)
		}
	}
}

func TestParseSuggestionJSONAndLegacy(t *testing.T) {
	jsonResult, err := ParseSuggestion(`{"command":"find . -type f","install_hint":""}`)
	if err != nil || jsonResult.Command != "find . -type f" {
		t.Fatalf("%+v %v", jsonResult, err)
	}
	legacy, err := ParseSuggestion("rg TODO\nINSTALL: brew install ripgrep")
	if err != nil || legacy.Command != "rg TODO" || legacy.InstallHint != "brew install ripgrep" {
		t.Fatalf("%+v %v", legacy, err)
	}
}

func TestParseSuggestionFencedJSON(t *testing.T) {
	result, err := ParseSuggestion("```json\n{\"command\":\"ffmpeg -ss 37 -i input.mp4 -t 1:34:00 -c copy output.mp4\",\"install_hint\":\"\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(result.Command, "json") || !strings.HasPrefix(result.Command, "ffmpeg ") {
		t.Fatalf("bad command: %+v", result)
	}
}

func TestParseSuggestionFindsJSONAfterText(t *testing.T) {
	result, err := ParseSuggestion("Here is the command:\n```json\n{\"command\":\"ls -la\",\"install_hint\":\"\"}\n```")
	if err != nil || result.Command != "ls -la" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestParseSuggestionRejectsFormatLabel(t *testing.T) {
	if _, err := ParseSuggestion("json"); err == nil {
		t.Fatal("expected format-label rejection")
	}
}

func TestConfirmDefaultsToNo(t *testing.T) {
	for _, input := range []string{"\n", "no\n", "anything\n"} {
		var output bytes.Buffer
		approved, err := Confirm(strings.NewReader(input), &output)
		if err != nil || approved {
			t.Fatalf("input %q approved=%v err=%v", input, approved, err)
		}
	}
	approved, err := Confirm(strings.NewReader("yes\n"), &bytes.Buffer{})
	if err != nil || !approved {
		t.Fatalf("yes approved=%v err=%v", approved, err)
	}
}
