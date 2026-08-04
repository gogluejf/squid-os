# EPIC: Provider Authentication & Dialect Framework
Why: Current codebase has no credential support -- Engine fires bare HTTP requests with no auth header. Need scalable provider architecture supporting API keys and OAuth, with per-provider dialect implementations, starting with OpenAI.
Outcomes: OpenAI OAuth (no-browser) and API key auth working; extensible provider interface for adding Gemini/Anthropic/Fireworks/OpenRouter; naming consistency across Provider/Endpoint/ModelEntry

## MILESTONE: 1 - Provider Data Model
Pattern: Interface Segregation, Strategy
Objective: Replace ProviderConfig with a proper Provider struct that carries auth method, dialect, credentials, and callback hooks
Success: ProviderConfig renamed to Provider with AuthMethod, Dialect, Credentials fields; old endpoints.json format migrated
Diagram: classDiagram
    class Provider {
        +string Name
        +string BaseURL
        +AuthMethod AuthMethod
        +string Dialect
        +ProviderCredentials Credentials
        +ModelsURL() string
        +ChatURL() string
    }
    class ProviderCredentials {
        +string APIKey
        +OAuthCreds OAuth
    }
    class OAuthCreds {
        +string AccessToken
        +string RefreshToken
        +time.Time ExpiresAt
    }
    class AuthMethod  {
        none
        api_key
        oauth
    }
    Provider --> ProviderCredentials
    ProviderCredentials --> OAuthCreds

### TASK: 1.1 - Provider struct with auth and dialect fields
Type: refactor
What: Replace ProviderConfig with Provider struct. AuthMethod is an array of supported methods, user picks one via ActiveAuthMethod in credentials. Known providers have hardcoded URLs, custom providers need BaseURL from user.
Why: Establish the data model for extensible provider authentication and API dialect support
Files: ~ internal/config/endpoints.go
Snippet: package config\n\ntype AuthMethod string\n\nconst (\n\tAuthNone   AuthMethod = "none"\n\tAuthAPIKey AuthMethod = "api_key"\n\tAuthOAuth  AuthMethod = "oauth"\n)\n\ntype Dialect string\n\nconst (\n\tDialectOpenAICompatible Dialect = "openai"\n\tDialectAnthropic        Dialect = "anthropic"\n\tDialectGemini           Dialect = "gemini"\n)\n\ntype Provider struct {\n\tName             string            `json:"name"`\n\tBaseURL          string            `json:"base_url,omitempty"`\n\tSupportedAuth    []AuthMethod      `json:"supported_auth"`\n\tDialect          Dialect           `json:"dialect"`\n\tCredentials      *ProviderCreds    `json:"credentials,omitempty"`\n\tNeedsConfig      bool              `json:"needs_config,omitempty"`\n}\n\ntype ProviderCreds struct {\n\tActiveAuthMethod AuthMethod    `json:"active_auth_method"`\n\tAPIKey           string        `json:"api_key,omitempty"`\n\tOAuth            *OAuthCreds   `json:"oauth,omitempty"`\n}\n\ntype OAuthCreds struct {\n\tAccessToken  string    `json:"access_token"`\n\tRefreshToken string    `json:"refresh_token"`\n\tExpiresAt    time.Time `json:"expires_at"`\n}\n\n// IsConfigured returns true if the provider has valid credentials to use\nfunc (p Provider) IsConfigured() bool {\n\tif p.Credentials == nil {\n\t\treturn false\n\t}\n\tif len(p.SupportedAuth) == 0 {\n\t\treturn true // no auth needed\n\t}\n\tswitch p.Credentials.ActiveAuthMethod {\n\tcase AuthAPIKey:\n\t\treturn p.Credentials.APIKey != ""\n\tcase AuthOAuth:\n\t\treturn p.Credentials.OAuth != nil && p.Credentials.OAuth.AccessToken != ""\n\tdefault:\n\t\treturn false\n\t}\n}\n\n// NeedsURL returns true if provider requires a user-provided base URL\nfunc (p Provider) NeedsURL() bool {\n\treturn p.IsKnownProvider() == false\n}\n\n// IsKnownProvider returns true for built-in providers with hardcoded URLs\nfunc (p Provider) IsKnownProvider() bool {\n\tknown := []string{"openai", "openai-codex", "anthropic", "gemini", "fireworks", "openrouter"}\n\tfor _, k := range known {\n\t\tif p.Name == k {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n\nfunc (p Provider) ChatURL() string {\n\tif p.IsKnownProvider() {\n\t\treturn p.getKnownChatURL()\n\t}\n\tif p.Dialect == DialectAnthropic {\n\t\treturn p.BaseURL + "/v1/messages"\n\t}\n\treturn p.BaseURL + "/v1/chat/completions"\n}\n\nfunc (p Provider) ModelsURL() string {\n\tif p.IsKnownProvider() {\n\t\treturn p.getKnownModelsURL()\n\t}\n\tif p.Dialect == DialectOpenAICompatible {\n\t\treturn p.BaseURL + "/v1/models"\n\t}\n\treturn ""\n}
Acceptance: Provider has SupportedAuth []AuthMethod (array of what the provider supports)
Acceptance: ProviderCreds has ActiveAuthMethod (single chosen method) plus APIKey/OAuth
Acceptance: IsConfigured checks if the active method has valid credentials
Acceptance: NeedsURL returns true for custom providers, false for known ones
Acceptance: IsKnownProvider identifies built-in providers with hardcoded URLs
Acceptance: ChatURL/ModelsURL use hardcoded URLs for known providers, BaseURL for custom
Verification: cd ~/src/squid-os && go build ./...

### TASK: 1.2 - Migrate EndpointsConfig and update all call sites
Type: refactor
What: Update EndpointsConfig with new Provider struct. Default endpoints include vllm/ollama (need URL config) and openai (hardcoded URL, supports api_key + oauth). All call sites updated.
Why: Ensure the entire codebase uses the new Provider model instead of the flat ProviderConfig
Files: ~ internal/config/endpoints.go
Files: ~ internal/app/stream.go
Files: ~ internal/app/app.go
Files: ~ internal/app/model.go
Files: ~ internal/chat/models.go
Files: ~ internal/headless/headless.go
Snippet: type EndpointsConfig struct {\n\tProviders []Provider `json:"providers"`\n}\n\nfunc DefaultEndpoints() EndpointsConfig {\n\treturn EndpointsConfig{\n\t\tProviders: []Provider{\n\t\t\t{\n\t\t\t\tName:          "vllm",\n\t\t\t\tBaseURL:       "",\n\t\t\t\tSupportedAuth: []AuthMethod{AuthNone},\n\t\t\t\tDialect:       DialectOpenAICompatible,\n\t\t\t\tNeedsConfig:   true, // needs URL from user\n\t\t\t},\n\t\t\t{\n\t\t\t\tName:          "ollama",\n\t\t\t\tBaseURL:       "",\n\t\t\t\tSupportedAuth: []AuthMethod{AuthNone},\n\t\t\t\tDialect:       DialectOpenAICompatible,\n\t\t\t\tNeedsConfig:   true, // needs URL from user\n\t\t\t},\n\t\t\t{\n\t\t\t\tName:          "openai",\n\t\t\t\tSupportedAuth: []AuthMethod{AuthAPIKey, AuthOAuth},\n\t\t\t\tDialect:       DialectOpenAICompatible,\n\t\t\t},\n\t\t},\n\t}\n}\n\nfunc ResolveProvider(endpoints EndpointsConfig, name string) *Provider {\n\tfor i, p := range endpoints.Providers {\n\t\tif p.Name == name {\n\t\t\treturn &endpoints.Providers[i]\n\t\t}\n\t}\n\treturn nil\n}
Acceptance: DefaultEndpoints returns vllm and ollama with AuthNone and DialectOpenAICompatible
Acceptance: ResolveProvider finds by name, returns nil if not found
Acceptance: All call sites in stream.go, headless.go, models.go updated from ProviderConfig to Provider
Acceptance: No compile errors across entire codebase
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 2 - Provider Interface
Pattern: Strategy, Interface Segregation
Objective: Define a Provider interface with callback methods that each dialect implements -- avoids big switch statements, follows existing component callback pattern
Success: Provider interface defined with PrepareRequest, has dialect-specific implementations, engine dispatches via interface
Diagram: classDiagram
    class ProviderImpl {
        +PrepareRequest(req) error
        +GetAccessToken() string
        +NeedsAuth() bool
        +IsExpired() bool
        +Refresh() error
    }
    class OpenAIProvider {
        +PrepareRequest(req) error
        +GetAccessToken() string
        +Refresh() error
    }
    class LocalProvider {
        +PrepareRequest(req) error
    }
    class ProviderRegistry {
        +Get(name) ProviderImpl
        +Register(p ProviderImpl)
    }
    class Engine {
        +provider ProviderImpl
        +Stream(ctx, msgs, tools) chan
    }
    ProviderImpl --|> OpenAIProvider
    ProviderImpl --|> LocalProvider
    Engine ..> ProviderImpl
    ProviderRegistry ..> ProviderImpl

### TASK: 2.1 - Define ProviderImpl interface and registry
Type: feature
What: Create chat/provider.go with ProviderImpl interface (PrepareRequest, GetAccessToken, NeedsAuth, IsExpired) and a registry map
Why: Each dialect implements its own auth preparation -- no switch statement, extensible like component callbacks
Files: + internal/chat/provider.go
Snippet: package chat\n\nimport (\n\t"net/http"\n\t"sync"\n)\n\ntype ProviderImpl interface {\n\t// PrepareRequest modifies the outgoing request with auth headers.\n\t// Returns error if credentials are missing or expired and cannot be refreshed.\n\tPrepareRequest(req *http.Request) error\n\t\n\t// GetAccessToken returns the current access token (for logging/debugging).\n\tGetAccessToken() string\n\t\n\t// NeedsAuth returns true if this provider requires authentication.\n\tNeedsAuth() bool\n\t\n\t// IsExpired returns true if OAuth credentials have expired.\n\tIsExpired() bool\n\t\n\t// Refresh attempts to refresh OAuth tokens. Returns error if not applicable or failed.\n\tRefresh() error\n}\n\nvar (\n\tproviderRegistry = make(map[string]ProviderImpl)\n\tregistryMu       sync.RWMutex\n)\n\nfunc RegisterProvider(name string, impl ProviderImpl) {\n\tregistryMu.Lock()\n\tdefer registryMu.Unlock()\n\tproviderRegistry[name] = impl\n}\n\nfunc GetProvider(name string) ProviderImpl {\n\tregistryMu.RLock()\n\tdefer registryMu.RUnlock()\n\treturn providerRegistry[name]\n}\n
Acceptance: ProviderImpl interface has PrepareRequest, GetAccessToken, NeedsAuth, IsExpired, Refresh methods
Acceptance: Registry uses sync.RWMutex for thread safety
Acceptance: RegisterProvider and GetProvider work correctly
Acceptance: No existing code breaks -- old Engine path still compiles
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.2 - Implement local/no-auth provider as default
Type: feature
What: Create a no-auth provider implementation that returns nil from PrepareRequest (passthrough for vllm/ollama), register it by default
Why: Existing local providers (vllm, ollama) need to continue working with no auth, serving as the base case
Files: + internal/chat/local_provider.go
Snippet: package chat\n\nimport "net/http"\n\ntype LocalProvider struct{}\n\nfunc (l *LocalProvider) PrepareRequest(req *http.Request) error { return nil }\nfunc (l *LocalProvider) GetAccessToken() string                { return "" }\nfunc (l *LocalProvider) NeedsAuth() bool                       { return false }\nfunc (l *LocalProvider) IsExpired() bool                       { return false }\nfunc (l *LocalProvider) Refresh() error                        { return nil }\n\nfunc init() {\n\tRegisterProvider("vllm", &LocalProvider{})\n\tRegisterProvider("ollama", &LocalProvider{})\n}\n
Acceptance: LocalProvider PrepareRequest is a no-op
Acceptance: NeedsAuth returns false
Acceptance: vllm and ollama registered in init
Acceptance: Existing Engine usage with local providers still works unchanged
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.3 - Wire Engine to use ProviderImpl for auth
Type: feature
What: Update Engine to accept a ProviderImpl, call PrepareRequest before each HTTP call, fall back to old behavior if no provider impl found
Why: Bridge the gap between the new provider interface and the existing Engine streaming logic
Files: ~ internal/chat/engine.go
Snippet: type Engine struct {\n\tChatURL  string\n\tModel    string\n\tThinking bool\n\tprovider ProviderImpl\n\tclient   *http.Client\n}\n\nfunc NewEngine(chatURL, model string, thinking bool, provider ProviderImpl) *Engine {\n\treturn &Engine{\n\t\tChatURL:  chatURL,\n\t\tModel:    model,\n\t\tThinking: thinking,\n\t\tprovider: provider,\n\t\tclient:   &http.Client{Timeout: 0},\n\t}\n}\n\n// In Stream(), after creating the request:\nif e.provider != nil {\n\tif err := e.provider.PrepareRequest(req); err != nil {\n\t\tch <- StreamEvent{Error: fmt.Errorf("provider auth: %w", err)}\n\t\treturn\n\t}\n}\n
Acceptance: Engine accepts ProviderImpl in NewEngine
Acceptance: PrepareRequest called before every outgoing HTTP request
Acceptance: Auth error returns StreamEvent with Error, does not panic
Acceptance: Backward compatible -- nil provider works as before (no auth header)
Acceptance: All call sites of NewEngine in stream.go and headless.go updated
Verification: cd ~/src/squid-os && go build ./...

### TASK: 2.4 - Generic Sequence component for multi-step wizards
Type: feature
What: Create component/sequence.go — a tea.Model that chains multiple child components (Input, Picker, Question) with a shared context map
Why: Reusable wizard abstraction for provider auth and future multi-step flows (tool questions, skill install). Components remain standalone and unchanged.
Files: + internal/ui/component/sequence.go
Snippet: package component\n\nimport (\n\ttea "github.com/charmbracelet/bubbletea"\n)\n\ntype SequenceStep struct {\n\tKey       string     // name for the result in shared context\n\tComponent tea.Model  // child component to run\n}\n\ntype Sequence struct {\n\tSteps    []SequenceStep\n\tCurrent  int\n\tContext  map[string]any  // accumulated results keyed by Step.Key\n\tOnDone   func(map[string]any) tea.Cmd\n\tOnCancel func() tea.Cmd\n}\n\nfunc (s *Sequence) Init() tea.Cmd {\n\treturn s.Steps[s.Current].Component.Init()\n}\n\nfunc (s *Sequence) Update(msg tea.Msg) (tea.Model, tea.Cmd) {\n\tcurrent := &s.Steps[s.Current]\n\tnewChild, cmd := current.Component.Update(msg)\n\t\n\t// Check if child is done (returned a different type or nil cmd with done signal)\n\tif isDone(newChild) {\n\t\t// Extract result and store in context under step's key\n\t\ts.Context[current.Key] = extractResult(newChild)\n\t\t\n\t\tif s.Current+1 < len(s.Steps) {\n\t\t\ts.Current++\n\t\t\treturn s, s.Steps[s.Current].Component.Init()\n\t\t}\n\t\treturn s, s.OnDone(s.Context)\n\t}\n\t\n\tcurrent.Component = newChild\n\treturn s, cmd\n}\n\nfunc (s *Sequence) View() string {\n\treturn s.Steps[s.Current].Component.View()\n}\n\n// isDone checks if a child component signaled completion\n// Each component type has its own signal (e.g., Input returns empty cmd on enter)\nfunc isDone(m tea.Model) bool {\n\t// Implementation depends on how child signals completion\n\t// For now: if Update returns the same model type but with a Done flag\n}\n\n// extractResult pulls the output value from a completed component\nfunc extractResult(m tea.Model) any {\n\tswitch c := m.(type) {\n\tcase *Input:\n\t\treturn c.Value\n\tcase *Picker:\n\t\treturn c.Selected\n\tcase *Question:\n\t\treturn struct{ Index int; Text string }{c.Selected, c.Instructions}\n\tdefault:\n\t\treturn nil\n\t}\n}\n
Acceptance: Sequence implements tea.Model (Init, Update, View)
Acceptance: Steps are defined as SequenceStep with Key name and child Component
Acceptance: Shared context map stores results keyed by each step's Key
Acceptance: On step completion: result extracted, stored in context, advances to next step
Acceptance: On all steps done: calls OnDone with full context map
Acceptance: OnCancel callback supported
Acceptance: Child components remain unchanged — they don't know about Sequence
Acceptance: Works with existing Input, Picker, Question components
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 3 - OpenAI OAuth (No-Browser)
Pattern: PKCE, Strategy
Objective: Implement OpenAI OAuth 2.0 PKCE flow without browser automation -- user copies URL, pastes authorization code back in TUI
Success: User can authenticate with OpenAI subscription via paste-code flow, tokens auto-refresh, works on server and desktop
Diagram: sequenceDiagram
    participant U as User
    participant T as TUI
    participant O as OpenAI Auth Server
    participant A as OpenAI API

    T->>U: Display auth URL + PKCE challenge
    U->>O: Opens URL in browser, logs in
    O->>U: Redirects with ?code=XYZ
    U->>T: Pastes authorization code
    T->>O: Exchanges code + code_verifier for tokens
    O->>T: Returns access_token, refresh_token, expires_in
    T->>T: Stores credentials in endpoints.json
    T->>A: API call with Bearer token
    A->>T: Response
    Note over T: Auto-refresh when expires_at within 5min

### TASK: 3.1 - OpenAI OAuth PKCE implementation
Type: feature
What: Create chat/openai_provider.go with full PKCE OAuth flow: code challenge generation, token exchange, auto-refresh, and PrepareRequest that injects Bearer token
Why: Enable OpenAI subscription authentication -- the primary auth use case for cloud providers
Files: + internal/chat/openai_provider.go
Snippet: package chat\n\nimport (\n\t"crypto/rand"\n\t"crypto/sha256"\n\t"encoding/base64"\n\t"fmt"\n\t"net/http"\n\t"net/url"\n\t"time"\n)\n\nconst (\n\topenaiClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"\n\topenaiAuthURL       = "https://auth.openai.com/oauth/authorize"\n\topenaiTokenURL      = "https://auth.openai.com/oauth/token"\n\topenaiBaseURL       = "https://api.openai.com"\n)\n\ntype OpenAIProvider struct {\n\tcodeVerifier string\n\tcreds        *ProviderCreds\n}\n\nfunc NewOpenAIProviderWithKey(apiKey string) *OpenAIProvider {\n\treturn &OpenAIProvider{\n\t\tcreds: &ProviderCreds{APIKey: apiKey},\n\t}\n}\n\n// StartOAuth returns the authorization URL the user must visit.\nfunc (o *OpenAIProvider) StartOAuth(redirectURI string) (authURL string, err error) {\n\t// Generate 32-byte code verifier\n\to.codeVerifier = generatePKCEVerifier()\n\tchallenge := generateCodeChallenge(o.codeVerifier)\n\t\n\tparams := url.Values{\n\t\t"client_id":                {openaiClientID},\n\t\t"response_type":            {"code"},\n\t\t"code_challenge":           {challenge},\n\t\t"code_challenge_method":    {"S256"},\n\t\t"redirect_uri":             {redirectURI},\n\t}\n\treturn openaiAuthURL + "?" + params.Encode(), nil\n}\n\n// FinishOAuth exchanges the authorization code for tokens.\nfunc (o *OpenAIProvider) FinishOAuth(code string) error {\n\t// POST to token endpoint with code + code_verifier\n\t// Store returned access_token, refresh_token, expires_at\n}\n\nfunc (o *OpenAIProvider) PrepareRequest(req *http.Request) error {\n\ttoken := o.getCurrentToken()\n\tif token == "" {\n\t\treturn fmt.Errorf("openai: no credentials configured")\n\t}\n\treq.Header.Set("Authorization", "Bearer "+token)\n\treturn nil\n}\n\nfunc (o *OpenAIProvider) Refresh() error {\n\t// If API key mode, nothing to refresh\n\t// If OAuth mode, use refresh_token to get new tokens\n}\n\nfunc generatePKCEVerifier() string {\n\tb := make([]byte, 32)\n\trand.Read(b)\n\treturn base64.RawURLEncoding.EncodeToString(b)\n}\n\nfunc generateCodeChallenge(verifier string) string {\n\th := sha256.Sum256([]byte(verifier))\n\treturn base64.RawURLEncoding.EncodeToString(h[:])\n}\n
Acceptance: PKCE code verifier is 32 random bytes, base64url encoded
Acceptance: Code challenge is SHA256 of verifier, base64url encoded
Acceptance: StartOAuth returns full authorization URL with correct OpenAI client ID
Acceptance: FinishOAuth exchanges code for tokens and stores them
Acceptance: PrepareRequest injects Bearer token (OAuth or API key)
Acceptance: Refresh swaps expired OAuth token using refresh_token
Acceptance: Returns error when no credentials configured
Verification: cd ~/src/squid-os && go build ./...

### TASK: 3.2 - OpenAI provider registration and credential persistence
Type: feature
What: Register openai and openai-codex providers, wire credential loading/saving from endpoints.json ProviderCreds into OpenAIProvider on init
Why: Credentials survive across sessions -- user doesn't re-auth on every restart
Files: ~ internal/chat/openai_provider.go
Files: ~ internal/config/endpoints.go
Snippet: // In LoadEndpoints or a new init function:\nfunc LoadProviderImpl(p Provider) ProviderImpl {\n\tswitch p.Name {\n\tcase "openai", "openai-codex":\n\t\timpl := &OpenAIProvider{}\n\t\tif p.Credentials != nil {\n\t\t\timpl.creds = p.Credentials\n\t\t}\n\t\treturn impl\n\tdefault:\n\t\treturn &LocalProvider{}\n\t}\n}\n\n// Save credentials back after OAuth completes:\nfunc (o *OpenAIProvider) GetCredentials() *ProviderCreds {\n\treturn o.creds\n}\n
Acceptance: LoadProviderImpl returns OpenAIProvider for openai/openai-codex names
Acceptance: Existing OAuth credentials from endpoints.json are loaded into provider
Acceptance: New credentials can be marshaled back to endpoints.json
Acceptance: Local providers still return LocalProvider
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 4 - TUI Auth Prompt
Pattern: Component, State Machine
Objective: Add a TUI prompt component that guides user through provider setup on first use -- displays auth URL, collects paste-back code, or collects API key directly
Success: User sees guided prompt on first use of an auth-required provider, completes setup in TUI, credentials saved
Diagram: stateDiagram-v2
    [*] --> DetectMissingCreds
    DetectMissingCreds --> ShowAuthOptions: user selects provider
    ShowAuthOptions --> EnterAPIKey: user chooses api_key
    ShowAuthOptions --> ShowOAuthURL: user chooses oauth
    EnterAPIKey --> ValidateKey: user pastes key
    ShowOAuthURL --> WaitForCode: user copies URL, opens browser
    WaitForCode --> ExchangeCode: user pastes auth code
    ValidateKey --> SaveCreds: key accepted
    ExchangeCode --> SaveCreds: tokens received
    SaveCreds --> [*]

### TASK: 4.1 - Provider auth wizard using Sequence component
Type: feature
What: Build provider auth flow in app layer using Sequence component: URL entry → auth method picker → credential entry. No new component file needed.
Why: Leverage generic Sequence component for clean multi-step auth wizard. Reuses existing Input, Picker, and Question components.
Files: + internal/app/auth_setup.go
Snippet: package app\n\n// buildAuthWizard creates a Sequence for provider configuration:\n// Step 1: If custom provider (vllm/ollama), prompt for BaseURL via Input\n// Step 2: If multiple auth methods, show Picker to choose method\n// Step 3: If API key, show Input for key entry\n// Step 3: If OAuth, show Question with URL, then Input for code\n// OnDone: save credentials, re-scan models\nfunc (m *Model) buildAuthWizard(p *config.Provider) *component.Sequence {\n\tvar steps []component.SequenceStep\n\t\n\tif p.NeedsURL() {\n\t\tsteps = append(steps, component.SequenceStep{\n\t\t\tKey: "baseURL",\n\t\t\tComponent: &component.Input{\n\t\t\t\tLabel: "Enter " + p.Name + " base URL (e.g., http://localhost:8080)",\n\t\t\t},\n\t\t})\n\t}\n\t\n\tif len(p.SupportedAuth) > 1 {\n\t\tmethods := []string{}\n\t\tfor _, a := range p.SupportedAuth {\n\t\t\tmethods = append(methods, string(a))\n\t\t}\n\t\tsteps = append(steps, component.SequenceStep{\n\t\t\tKey: "authMethod",\n\t\t\tComponent: &component.Picker{\n\t\t\t\tTitle:   "Choose authentication method for " + p.Name,\n\t\t\t\tOptions: methods,\n\t\t\t},\n\t\t})\n\t}\n\t\n\t// Auth step depends on chosen method or single available method\n\t// (built dynamically based on prior steps — see buildAuthStep)\n\t\n\treturn &component.Sequence{\n\t\tSteps: steps,\n\t\tOnDone: func(ctx map[string]any) tea.Cmd {\n\t\t\treturn m.onProviderConfigComplete(p, ctx)\n\t\t},\n\t\tOnCancel: func() tea.Cmd {\n\t\t\treturn m.returnToModelPicker()\n\t\t},\n\t}\n}\n\nfunc (m *Model) onProviderConfigComplete(p *config.Provider, ctx map[string]any) tea.Cmd {\n\t// Apply context values to provider\n\tif baseURL, ok := ctx["baseURL"].(string); ok {\n\t\tp.BaseURL = baseURL\n\t}\n\tif method, ok := ctx["authMethod"].(string); ok {\n\t\tp.Credentials = &config.ProviderCreds{\n\t\t\tActiveAuthMethod: config.AuthMethod(method),\n\t\t}\n\t\tif method == "api_key" {\n\t\t\tp.Credentials.APIKey = ctx["apiKey"].(string)\n\t\t} else if method == "oauth" {\n\t\t\t// Trigger OAuth flow with the code\n\t\t\t// (handled separately as OAuth needs network call)\n\t\t}\n\t}\n\t\n\t// Save and re-scan\n\tm.saveProvider(*p)\n\tm.refreshModels()\n\treturn m.setChatMode()\n}\n
Acceptance: Uses Sequence component to chain auth steps
Acceptance: Custom providers (vllm/ollama) prompt for BaseURL via Input step
Acceptance: Providers with multiple auth methods show Picker step
Acceptance: Providers with single auth method skip Picker, go straight to credential
Acceptance: API key mode uses Input step with masked display
Acceptance: OAuth mode shows URL via Question, then Input for code paste
Acceptance: OnDone saves credentials and re-scans model list
Acceptance: OnCancel returns to model picker without saving
Acceptance: No new component file — logic in app layer using existing components
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.2 - Wire auth prompt into provider selection flow
Type: feature
What: Before sending message, check if provider is fully configured. If not, show config wizard. Also auto-refresh expired OAuth tokens before sending.
Why: Integrate the auth prompt into the existing model/provider selection so user is guided through setup at the right moment
Files: ~ internal/app/model.go
Files: ~ internal/app/stream.go
Snippet: // Before sending message, check if provider is configured:\nfunc (m *Model) ensureProviderConfigured() (bool, tea.Cmd) {\n\tp := config.ResolveProvider(m.endpoints, m.settings.Provider)\n\tif p == nil {\n\t\treturn false, nil\n\t}\n\t\n\tif !p.IsConfigured() {\n\t\t// Show config wizard\n\t\treturn true, m.showProviderConfig(p)\n\t}\n\t\n\t// Check if OAuth creds are about to expire\n\timpl := chat.LoadProviderImpl(*p)\n\tif impl != nil && impl.IsExpired() {\n\t\tif err := impl.Refresh(); err != nil {\n\t\t\t// Refresh failed, show re-config wizard\n\t\t\treturn true, m.showProviderConfig(p)\n\t\t}\n\t\t// Refresh succeeded, save updated creds\n\t\tm.saveProvider(*p)\n\t}\n\treturn false, nil\n}
Acceptance: sendMessage checks if provider IsConfigured before sending
Acceptance: Unconfigured provider triggers config wizard instead of sending
Acceptance: Expired OAuth tokens auto-refresh before sending
Acceptance: Failed refresh triggers config wizard for re-auth
Acceptance: Config cancel gracefully returns without sending
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.3 - Wire model scanning through ProviderImpl auth
Type: feature
What: ScanModels returns sentinel entries for unconfigured providers (<not configured>) and expired creds (<auth expired>). Both trigger the universal config wizard.
Why: Model scanner needs auth to fetch models from authenticated providers, and must surface auth-required providers as selectable placeholders in the picker
Files: ~ internal/chat/models.go
Files: ~ internal/chat/provider.go
Snippet: func ScanModels(ctx context.Context, endpoints config.EndpointsConfig) []ModelEntry {\n\tvar mu sync.Mutex\n\tvar models []ModelEntry\n\tvar wg sync.WaitGroup\n\n\tfor _, provider := range endpoints.Providers {\n\t\twg.Add(1)\n\t\tgo func(p config.Provider) {\n\t\t\tdefer wg.Done()\n\t\t\t\n\t\t\t// If provider needs initial config (URL or auth), return sentinel\n\t\t\tif !p.IsConfigured() {\n\t\t\t\tmu.Lock()\n\t\t\t\tmodels = append(models, ModelEntry{\n\t\t\t\t\tID:          "<not configured>",\n\t\t\t\t\tProvider:    p.Name,\n\t\t\t\t\tNeedsConfig: true,\n\t\t\t\t})\n\t\t\t\tmu.Unlock()\n\t\t\t\treturn\n\t\t\t}\n\t\t\t\n\t\t\timpl := LoadProviderImpl(p)\n\t\t\tentries, err := FetchModelsDetailWithAuth(ctx, p, impl)\n\t\t\tif err != nil {\n\t\t\t\t// If 401, creds may be expired - return sentinel\n\t\t\t\tif isAuthError(err) {\n\t\t\t\t\tmu.Lock()\n\t\t\t\t\tmodels = append(models, ModelEntry{\n\t\t\t\t\t\tID:          "<auth expired>",\n\t\t\t\t\t\tProvider:    p.Name,\n\t\t\t\t\t\tNeedsConfig: true,\n\t\t\t\t\t})\n\t\t\t\t\tmu.Unlock()\n\t\t\t\t}\n\t\t\t\treturn\n\t\t\t}\n\t\t\t\n\t\t\tmu.Lock()\n\t\t\tmodels = append(models, entries...)\n\t\t\tmu.Unlock()\n\t\t}(provider)\n\t}\n\twg.Wait()\n\treturn models\n}
Acceptance: Unconfigured providers return <not configured> sentinel
Acceptance: Providers with expired/invalid creds return <auth expired> sentinel
Acceptance: Both sentinel types have NeedsConfig flag set to true
Acceptance: Local providers with no auth need still show <not configured> if URL missing
Acceptance: ModelEntry has NeedsConfig flag (replaces NeedsAuth) for broader coverage
Verification: cd ~/src/squid-os && go build ./...

### TASK: 4.4 - Picker shows auth-required placeholders and triggers auth prompt
Type: feature
What: Model picker shows <not configured> and <auth expired> sentinels. Selecting one opens the universal config wizard. On completion, saves provider and re-scans.
Why: User needs to discover and authenticate providers through the model picker since that is the primary interaction point
Files: ~ internal/app/model.go
Snippet: // In model picker display logic:\nfunc renderModelEntry(e ModelEntry) string {\n\tif e.NeedsConfig {\n\t\tlabel := e.ID\n\t\tif e.ID == "<auth expired>" {\n\t\t\tlabel = "re-authenticate"\n\t\t}\n\t\treturn fmt.Sprintf("  %s: click to %s", e.Provider, label)\n\t}\n\treturn fmt.Sprintf("  %s: %s", e.Provider, e.ID)\n}\n\n// On model selection:\nfunc (m *Model) onModelSelected(entry ModelEntry) (Model, tea.Cmd) {\n\tif entry.NeedsConfig {\n\t\t// Trigger config wizard for this provider\n\t\tp := config.ResolveProvider(m.endpoints, entry.Provider)\n\t\treturn m, m.showProviderConfig(p)\n\t}\n\t// Normal model selection\n\tm.settings.Provider = entry.Provider\n\tm.settings.Model = entry.ID\n\treturn m, nil\n}\n\n// After successful config, re-scan models:\nfunc (m *Model) onProviderConfigComplete(updated config.Provider) (Model, tea.Cmd) {\n\t// Save updated provider back to endpoints\n\tm.saveProvider(updated)\n\t\n\tctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)\n\tdefer cancel()\n\tmodels := chat.ScanModels(ctx, m.endpoints)\n\tm.populateModelList(models)\n\treturn m, nil\n}
Acceptance: Picker displays <not configured> for providers missing URL or creds
Acceptance: Picker displays <auth expired> for providers with invalid creds
Acceptance: Selecting sentinel opens provider config wizard
Acceptance: Wizard completion saves provider and re-scans model list
Acceptance: Normal model selection unchanged for configured providers
Verification: cd ~/src/squid-os && go build ./...

## MILESTONE: 5 - Error Handling & Fallback
Pattern: Circuit Breaker
Objective: Handle auth failures gracefully -- expired tokens trigger re-prompt, 401 errors trigger credential refresh or re-auth
Success: Expired tokens auto-refresh before retry, 401 errors prompt for re-auth, unsupported providers return clear error
Diagram: flowchart LR
    A[HTTP Request] --> B{Status Code}
    B -->|200| C[Success]
    B -->|401| D{Has RefreshToken}
    D -->|Yes| E[Refresh Token]
    E --> F{Retry}
    F -->|200| C
    F -->|401| G[Show Re-auth Prompt]
    D -->|No| G
    B -->|403| H[Permission Error Message]
    B -->|Other| I[Standard Error]

### TASK: 5.1 - 401 detection and auto-refresh with re-auth prompt
Type: feature
What: In Engine Stream, detect 401 response, attempt token refresh, retry once, then return error that triggers re-auth prompt
Why: Expired tokens are common -- auto-refresh avoids bothering the user, re-auth prompt is the last resort
Files: ~ internal/chat/engine.go
Snippet: // In Stream(), after getting a non-200 response:\nif resp.StatusCode == http.StatusUnauthorized {\n\t// Try refreshing credentials\n\tif e.provider != nil && e.provider.IsExpired() {\n\t\tif err := e.provider.Refresh(); err == nil {\n\t\t\t// Retry with new token\n\t\t\treq2, _ := http.NewRequestWithContext(ctx, "POST", e.ChatURL, bytes.NewReader(body))\n\t\t\treq2.Header.Set("Content-Type", "application/json")\n\t\t\te.provider.PrepareRequest(req2)\n\t\t\t// ... repeat the request ...\n\t\t}\n\t}\n\t// If refresh failed or not available, return error that triggers re-auth\n\tch <- StreamEvent{Error: fmt.Errorf("authentication failed -- re-configure provider %s", e.providerName)}\n\treturn\n}\n
Acceptance: 401 response triggers token refresh attempt before showing error
Acceptance: Successful refresh retries the request with new token
Acceptance: Failed refresh returns error message that triggers re-auth prompt
Acceptance: Providers without refresh capability return clear error
Verification: cd ~/src/squid-os && go build ./...

### TASK: 5.2 - Unsupported provider error and placeholder error for non-openai dialects
Type: feature
What: When user configures a provider with a dialect that has no implementation yet (anthropic, gemini), return a clear error explaining it's not yet supported
Why: Scope is OpenAI only for now -- other providers should fail gracefully with a helpful message, not a cryptic panic
Files: ~ internal/chat/provider.go
Snippet: // In LoadProviderImpl:\nfunc LoadProviderImpl(p config.Provider) ProviderImpl {\n\tswitch p.Name {\n\tcase "openai", "openai-codex":\n\t\treturn newOpenAIFromConfig(p)\n\tdefault:\n\t\tif p.Dialect != config.DialectOpenAICompatible {\n\t\t\treturn &UnsupportedProvider{Dialect: p.Dialect}\n\t\t}\n\t\treturn &LocalProvider{}\n\t}\n}\n\ntype UnsupportedProvider struct {\n\tDialect string\n}\n\nfunc (u *UnsupportedProvider) PrepareRequest(req *http.Request) error {\n\treturn fmt.Errorf("provider dialect %q not yet supported -- only openai-compatible providers are currently available", u.Dialect)\n}\n// ... other interface methods return appropriate stubs\n
Acceptance: Anthropic dialect returns clear unsupported error from PrepareRequest
Acceptance: Gemini dialect returns clear unsupported error
Acceptance: OpenAI and local providers work normally
Acceptance: Error message guides user on what IS supported
Verification: cd ~/src/squid-os && go build ./...
