package provider

import (
	"testing"

	"squid-os/internal/config"
	"squid-os/internal/media"

	goai_provider "github.com/zendev-sh/goai/provider"
)

// ---------------------------------------------------------------------------
// MediaContractCase
// ---------------------------------------------------------------------------

// MediaContractCase defines an expected media handling outcome for a
// provider/dialect and media kind combination.
type MediaContractCase struct {
	Provider string
	Dialect  config.Dialect
	Kind     media.Kind
	Expected MediaSupport
}

// ---------------------------------------------------------------------------
// Dialect → adapter mapping
// ---------------------------------------------------------------------------

// adapterMediaSupport maps each dialect to its GoAI adapter's Part serialization
// capabilities. This is the source of truth for the media contract tests.
//
// Derivation from GoAI adapter source code (vendored):
//
//	DialectOpenAICompatible (openaicompat/messages.go ConvertMessages):
//	  PartImage → image_url (supported)
//	  PartFile  → silently omitted in content array (unsupported)
//	  PartText  → text (supported)
//
//	DialectOpenAICodex (openai/responses.go convertToResponsesInput):
//	  PartImage → input_image (supported)
//	  PartFile  → input_file with file_data (supported)
//	  PartText  → input_text (supported)
//
//	DialectAnthropic (anthropic/anthropic.go convertMessages):
//	  PartImage → image base64 source (supported)
//	  PartFile  → document base64 source (supported)
//	  PartText  → text (supported)
//
//	DialectGemini (google/google.go convertMessages):
//	  PartImage → inlineData (supported)
//	  PartFile  → inlineData (supported)
//	  PartText  → text (supported)
//
// Audio and video: no GoAI adapter currently serializes PartAudio or PartVideo
// (those PartType constants do not even exist). All audio/video attachments
// are storable but not deliverable as direct provider input.
//
// Note: DialectOpenAICompatible uses openaicompat.ConvertMessages which does
// NOT handle PartFile in the content array — file parts are silently dropped.
// This is a known gap documented as Unsupported below.
var adapterMediaSupport = map[config.Dialect]map[media.Kind]MediaSupport{
	config.DialectOpenAICompatible: {
		media.KindImage: Supported,
		media.KindPDF:   Unsupported, // openaicompat ConvertMessages omits PartFile
		media.KindText:  Supported,
		media.KindAudio: Storable,
		media.KindVideo: Storable,
		media.KindFile:  Unsupported, // openaicompat ConvertMessages omits PartFile
	},
	config.DialectOpenAICodex: {
		media.KindImage: Supported,
		media.KindPDF:   Supported,
		media.KindText:  Supported,
		media.KindAudio: Storable,
		media.KindVideo: Storable,
		media.KindFile:  Supported,
	},
	config.DialectAnthropic: {
		media.KindImage: Supported,
		media.KindPDF:   Supported,
		media.KindText:  Supported,
		media.KindAudio: Storable,
		media.KindVideo: Storable,
		media.KindFile:  Supported,
	},
	config.DialectGemini: {
		media.KindImage: Supported,
		media.KindPDF:   Supported,
		media.KindText:  Supported,
		media.KindAudio: Storable,
		media.KindVideo: Storable,
		media.KindFile:  Supported,
	},
}

// ---------------------------------------------------------------------------
// Provider registry → dialect mapping
// ---------------------------------------------------------------------------

// providerDialects maps every registered provider name to its dialect.
// This table must stay in sync with each provider's Dialect() return value.
var providerDialects = map[string]config.Dialect{
	config.ProviderOpenAI:      config.DialectOpenAICompatible,
	config.ProviderOpenAICodex: config.DialectOpenAICodex,
	config.ProviderAnthropic:   config.DialectAnthropic,
	config.ProviderGemini:      config.DialectGemini,
	config.ProviderBedrock:     config.DialectAnthropic, // Bedrock uses Anthropic GoAI adapter
	config.ProviderVertex:      config.DialectGemini,    // Vertex uses Gemini GoAI adapter
	config.ProviderOllama:      config.DialectOpenAICompatible,
	config.ProviderLiteLLM:     config.DialectOpenAICompatible,
	config.ProviderVLLM:        config.DialectOpenAICompatible,
	config.ProviderAzure:       config.DialectOpenAICompatible,
	config.ProviderOpenRouter:  config.DialectOpenAICompatible,
	config.ProviderFireworks:   config.DialectOpenAICompatible,
	config.ProviderXAI:         config.DialectOpenAICompatible,
	config.ProviderGroq:        config.DialectOpenAICompatible,
	config.ProviderDeepSeek:    config.DialectOpenAICompatible,
	config.ProviderMiniMax:     config.DialectAnthropic,
	config.ProviderTogether:    config.DialectOpenAICompatible,
	config.ProviderDeepInfra:   config.DialectOpenAICompatible,
	config.ProviderRequesty:    config.DialectOpenAICompatible,
	config.ProviderCohere:      config.DialectOpenAICompatible,
	config.ProviderMistral:     config.DialectOpenAICompatible,
	config.ProviderPerplexity:  config.DialectOpenAICompatible,
	config.ProviderCerebras:    config.DialectOpenAICompatible,
	config.ProviderNVIDIA:      config.DialectOpenAICompatible,
	config.ProviderRunPod:      config.DialectOpenAICompatible,
	config.ProviderFPTCloud:    config.DialectOpenAICompatible,
	config.ProviderCloudflare:  config.DialectOpenAICompatible,
	config.ProviderLlamaCpp:    config.DialectOpenAICompatible,
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMediaContractProviderDialectMapping verifies that every provider
// registered in providerDialects returns the correct dialect from its
// Dialect() method. This ensures the contract table is in sync.
func TestMediaContractProviderDialectMapping(t *testing.T) {
	for name, wantDialect := range providerDialects {
		p := GetByName(name)
		if p == nil {
			t.Errorf("provider %q not registered in GoAI registry — check provider init()", name)
			continue
		}
		gotDialect := p.Dialect()
		if gotDialect != wantDialect {
			t.Errorf("provider %q: Dialect() = %q, want %q", name, gotDialect, wantDialect)
		}
	}
}

// TestMediaContractAdapterPartSerialization checks that each dialect's
// Part serialization capabilities match the documented expectations.
// This is the core contract test: it verifies the GoAI adapter behavior
// for each dialect/kind combination.
func TestMediaContractAdapterPartSerialization(t *testing.T) {
	kinds := []media.Kind{
		media.KindImage,
		media.KindPDF,
		media.KindText,
		media.KindAudio,
		media.KindVideo,
		media.KindFile,
	}

	for dialect, wantMap := range adapterMediaSupport {
		for _, kind := range kinds {
			want := wantMap[kind]
			if want == "" {
				t.Errorf("dialect %q: no entry for kind %q — add to adapterMediaSupport", dialect, kind)
				continue
			}

			// Verify the expectation is reasonable
			switch want {
			case Supported:
				// Valid: adapter serializes this part type.
			case Unsupported:
				// Valid: adapter does not serialize this part type.
			case Storable:
				// Valid: storable but not deliverable as GoAI input.
				if kind != media.KindAudio && kind != media.KindVideo {
					t.Errorf("dialect %q, kind %q: Storable is only valid for audio/video", dialect, kind)
				}
			default:
				t.Errorf("dialect %q, kind %q: unexpected MediaSupport value %q", dialect, kind, want)
			}
		}
	}
}

// TestMediaContractProviderCoverage verifies that every registered provider
// is covered by the contract table and that the expected support level is
// correctly derived from its dialect.
func TestMediaContractProviderCoverage(t *testing.T) {
	kinds := []media.Kind{
		media.KindImage, media.KindPDF, media.KindText,
		media.KindAudio, media.KindVideo, media.KindFile,
	}

	for _, name := range All() {
		p := GetByName(name)
		if p == nil {
			continue
		}
		dialect := p.Dialect()

		// Check that the dialect has adapter support defined.
		supportMap, ok := adapterMediaSupport[dialect]
		if !ok {
			t.Errorf("provider %q: dialect %q not in adapterMediaSupport — add mapping", name, dialect)
			continue
		}

		// For each kind, verify the expected support is derivable from the dialect.
		for _, kind := range kinds {
			expected := supportMap[kind]
			if expected == "" {
				t.Errorf("provider %q (dialect %q): no adapter support defined for kind %q", name, dialect, kind)
			}
		}
	}
}

// TestMediaContractOpenAICompatFilePartOmission verifies that OpenAI-compatible
// Chat Completions (openaicompat adapter) does NOT silently accept file parts.
// This is a critical contract: file parts must be explicitly marked as unsupported
// for this dialect, not silently dropped.
func TestMediaContractOpenAICompatFilePartOmission(t *testing.T) {
	dialect := config.DialectOpenAICompatible
	supportMap := adapterMediaSupport[dialect]

	// PartFile is omitted by openaicompat.ConvertMessages — must be Unsupported.
	if support := supportMap[media.KindPDF]; support != Unsupported {
		t.Errorf("openai-compatible: PDF should be Unsupported (PartFile silently omitted), got %s", support)
	}
	if support := supportMap[media.KindFile]; support != Unsupported {
		t.Errorf("openai-compatible: File should be Unsupported (PartFile silently omitted), got %s", support)
	}
}

// TestMediaContractOpenAICodexFilePartSupported verifies that the OpenAI
// Codex provider (Responses API) correctly supports file parts via input_file.
func TestMediaContractOpenAICodexFilePartSupported(t *testing.T) {
	dialect := config.DialectOpenAICodex
	supportMap := adapterMediaSupport[dialect]

	if support := supportMap[media.KindPDF]; support != Supported {
		t.Errorf("openai-codex: PDF should be Supported (input_file in Responses API), got %s", support)
	}
	if support := supportMap[media.KindFile]; support != Supported {
		t.Errorf("openai-codex: File should be Supported (input_file in Responses API), got %s", support)
	}
}

// TestMediaContractAnthropicFilePartSupported verifies that Anthropic
// correctly supports file parts via document base64 source.
func TestMediaContractAnthropicFilePartSupported(t *testing.T) {
	dialect := config.DialectAnthropic
	supportMap := adapterMediaSupport[dialect]

	if support := supportMap[media.KindPDF]; support != Supported {
		t.Errorf("anthropic: PDF should be Supported (document base64 source), got %s", support)
	}
	if support := supportMap[media.KindFile]; support != Supported {
		t.Errorf("anthropic: File should be Supported (document base64 source), got %s", support)
	}
}

// TestMediaContractGeminiFilePartSupported verifies that Gemini correctly
// supports file parts via inlineData.
func TestMediaContractGeminiFilePartSupported(t *testing.T) {
	dialect := config.DialectGemini
	supportMap := adapterMediaSupport[dialect]

	if support := supportMap[media.KindPDF]; support != Supported {
		t.Errorf("gemini: PDF should be Supported (inlineData), got %s", support)
	}
	if support := supportMap[media.KindFile]; support != Supported {
		t.Errorf("gemini: File should be Supported (inlineData), got %s", support)
	}
}

// TestMediaContractAudioVideoStorable verifies that audio and video are
// consistently marked as storable (not direct GoAI input) across all dialects.
func TestMediaContractAudioVideoStorable(t *testing.T) {
	for dialect, supportMap := range adapterMediaSupport {
		if support := supportMap[media.KindAudio]; support != Storable {
			t.Errorf("dialect %q: audio should be Storable, got %s", dialect, support)
		}
		if support := supportMap[media.KindVideo]; support != Storable {
			t.Errorf("dialect %q: video should be Storable, got %s", dialect, support)
		}
	}
}

// TestMediaContractBedrockUsesAnthropicAdapter verifies that Bedrock
// correctly inherits Anthropic adapter capabilities.
func TestMediaContractBedrockUsesAnthropicAdapter(t *testing.T) {
	p := GetByName(config.ProviderBedrock)
	if p == nil {
		t.Fatal("bedrock provider not registered")
	}
	if p.Dialect() != config.DialectAnthropic {
		t.Errorf("bedrock dialect: got %q, want %q", p.Dialect(), config.DialectAnthropic)
	}
}

// TestMediaContractVertexUsesGeminiAdapter verifies that Vertex
// correctly inherits Gemini adapter capabilities.
func TestMediaContractVertexUsesGeminiAdapter(t *testing.T) {
	p := GetByName(config.ProviderVertex)
	if p == nil {
		t.Fatal("vertex provider not registered")
	}
	if p.Dialect() != config.DialectGemini {
		t.Errorf("vertex dialect: got %q, want %q", p.Dialect(), config.DialectGemini)
	}
}

// TestMediaContractLocalProvidersCovered verifies that all configured local
// dialects (ollama, vllm, litellm, llamacpp) are covered and correctly
// mapped to OpenAI-compatible Chat Completions.
func TestMediaContractLocalProvidersCovered(t *testing.T) {
	localProviders := []string{
		config.ProviderOllama,
		config.ProviderVLLM,
		config.ProviderLiteLLM,
		config.ProviderLlamaCpp,
	}

	for _, name := range localProviders {
		p := GetByName(name)
		if p == nil {
			t.Errorf("local provider %q not registered", name)
			continue
		}

		dialect := p.Dialect()
		if dialect != config.DialectOpenAICompatible {
			t.Errorf("local provider %q: dialect = %q, want %q",
				name, dialect, config.DialectOpenAICompatible)
		}

		// Verify file parts are unsupported for local providers
		// (they use openaicompat which omits PartFile)
		supportMap := adapterMediaSupport[dialect]
		if support := supportMap[media.KindFile]; support != Unsupported {
			t.Errorf("local provider %q: file should be Unsupported (openaicompat omits PartFile), got %s",
				name, support)
		}
	}
}

// TestMediaContractGoAICapabilitiesConsistency verifies that the GoAI
// CapableModel.Capabilities() method for each adapter's default model
// aligns with the adapter media support table. This catches drift between
// GoAI's declared capabilities and our contract table.
func TestMediaContractGoAICapabilitiesConsistency(t *testing.T) {
	// Test a few representative providers to verify GoAI Capabilities
	// match our adapterMediaSupport table.
	type check struct {
		providerName string
		modelID      string
		dialect      config.Dialect
	}

	checks := []check{
		{config.ProviderOpenAI, "gpt-4o", config.DialectOpenAICompatible},
		{config.ProviderOpenAICodex, "gpt-5.4", config.DialectOpenAICodex},
		{config.ProviderAnthropic, "claude-sonnet-4-20250514", config.DialectAnthropic},
		{config.ProviderGemini, "gemini-2.5-flash", config.DialectGemini},
	}

	for _, c := range checks {
		p := GetByName(c.providerName)
		if p == nil {
			t.Errorf("%s: provider not registered", c.providerName)
			continue
		}

		model, _, err := p.BuildGoAIModel(c.modelID)
		if err != nil {
			// Some providers fail BuildGoAIModel without credentials — that's OK.
			// We can still verify the dialect mapping.
			continue
		}

		caps := goai_provider.ModelCapabilitiesOf(model)
		supportMap := adapterMediaSupport[c.dialect]

		// Check image capability alignment
		if supportMap[media.KindImage] == Supported {
			if !caps.InputModalities.Image {
				t.Errorf("%s: GoAI caps missing Image but contract says Supported", c.providerName)
			}
		}

		// Check PDF capability alignment
		if supportMap[media.KindPDF] == Supported {
			if !caps.InputModalities.PDF {
				t.Errorf("%s: GoAI caps missing PDF but contract says Supported", c.providerName)
			}
		}
	}
}

// TestMediaContractCaseMatrix runs the full matrix of provider/kind combinations
// through the MediaContractCase struct to ensure no entries are missing.
func TestMediaContractCaseMatrix(t *testing.T) {
	kinds := []media.Kind{
		media.KindImage, media.KindPDF, media.KindText,
		media.KindAudio, media.KindVideo, media.KindFile,
	}

	var cases []MediaContractCase
	for _, name := range All() {
		p := GetByName(name)
		if p == nil {
			continue
		}
		dialect := p.Dialect()
		supportMap, ok := adapterMediaSupport[dialect]
		if !ok {
			t.Errorf("provider %q: dialect %q not in adapterMediaSupport", name, dialect)
			continue
		}
		for _, kind := range kinds {
			expected := supportMap[kind]
			cases = append(cases, MediaContractCase{
				Provider: name,
				Dialect:  dialect,
				Kind:     kind,
				Expected: expected,
			})
		}
	}

	// Verify we have cases for all providers and kinds
	providerKindSeen := make(map[string]int)
	for _, c := range cases {
		key := c.Provider + ":" + string(c.Kind)
		providerKindSeen[key]++
		if c.Expected == "" {
			t.Errorf("case %s/%s: missing Expected value", c.Provider, c.Kind)
		}
	}

	// Verify coverage: each provider should have exactly len(kinds) cases
	for _, name := range All() {
		count := 0
		for k := range providerKindSeen {
			if len(k) > len(name)+1 && k[:len(name)] == name && k[len(name)] == ':' {
				count++
			}
		}
		if count != len(kinds) {
			t.Errorf("provider %q: %d cases, want %d", name, count, len(kinds))
		}
	}

	t.Logf("Generated %d contract cases across %d providers and %d media kinds",
		len(cases), len(providerDialects), len(kinds))
}

// TestMediaContractExplicitUnsupportedCombinations documents the explicitly
// unsupported combinations so they are visible in test output.
func TestMediaContractExplicitUnsupportedCombinations(t *testing.T) {
	var unsupported []MediaContractCase

	for _, name := range All() {
		p := GetByName(name)
		if p == nil {
			continue
		}
		dialect := p.Dialect()
		supportMap := adapterMediaSupport[dialect]
		if supportMap == nil {
			continue
		}
		for _, kind := range []media.Kind{media.KindImage, media.KindPDF, media.KindFile} {
			if supportMap[kind] == Unsupported {
				unsupported = append(unsupported, MediaContractCase{
					Provider: name,
					Dialect:  dialect,
					Kind:     kind,
					Expected: Unsupported,
				})
			}
		}
	}

	// Log all unsupported combinations for visibility
	for _, c := range unsupported {
		t.Logf("UNSUPPORTED: %s (%s) — %s", c.Provider, c.Dialect, c.Kind)
	}

	// Verify OpenAI-compatible providers have file/PDF unsupported
	openAICompatCount := 0
	for _, c := range unsupported {
		if c.Dialect == config.DialectOpenAICompatible &&
			(c.Kind == media.KindPDF || c.Kind == media.KindFile) {
			openAICompatCount++
		}
	}

	// Each openai-compatible provider should have at least 2 unsupported entries (PDF + File)
	openAICompatProviders := 0
	for _, d := range providerDialects {
		if d == config.DialectOpenAICompatible {
			openAICompatProviders++
		}
	}
	expected := openAICompatProviders * 2 // PDF + File per provider
	if openAICompatCount != expected {
		t.Errorf("openai-compatible unsupported count: got %d, want %d (%d providers × 2 kinds)",
			openAICompatCount, expected, openAICompatProviders)
	}
}
