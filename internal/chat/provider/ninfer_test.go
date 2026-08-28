package provider

import (
	"reflect"
	"testing"

	"squid-os/internal/config"
)

func TestNInferProviderMetadata(t *testing.T) {
	p := GetByName(config.ProviderNInfer)
	if p == nil {
		t.Fatal("ninfer provider is not registered")
	}
	if got := p.DefaultBaseURL(); got != "http://localhost:8080" {
		t.Fatalf("DefaultBaseURL() = %q, want %q", got, "http://localhost:8080")
	}
	if got := p.Dialect(); got != config.DialectOpenAICompatible {
		t.Fatalf("Dialect() = %q, want %q", got, config.DialectOpenAICompatible)
	}
}

func TestNInferThinkingOptionIsTopLevel(t *testing.T) {
	p := GetByName(config.ProviderNInfer)

	for _, thinking := range []bool{false, true} {
		want := map[string]any{"enable_thinking": thinking}
		got := p.RequestProviderOptions("qwen3.8-27b", thinking)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RequestProviderOptions(_, %t) = %#v, want %#v", thinking, got, want)
		}
		if _, nested := got["chat_template_kwargs"]; nested {
			t.Errorf("RequestProviderOptions(_, %t) must not contain chat_template_kwargs", thinking)
		}
	}
}

func TestNInferBuildModelRequiresModel(t *testing.T) {
	p := GetByName(config.ProviderNInfer)
	if _, _, err := p.BuildGoAIModel(""); err == nil {
		t.Fatal("BuildGoAIModel(\"\") returned nil error")
	}
}
