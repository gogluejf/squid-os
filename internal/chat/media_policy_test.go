package chat

import (
	"errors"
	"testing"

	"squid-os/internal/media"

	goai "github.com/zendev-sh/goai"
	goai_provider "github.com/zendev-sh/goai/provider"
)

func TestMediaPolicyDecide(t *testing.T) {

	// Nil capabilities → attempt everything (unknown model)
	if d := MediaDecisionFor(nil, media.KindImage); d != MediaAttempt {
		t.Errorf("nil caps image: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(nil, media.KindPDF); d != MediaAttempt {
		t.Errorf("nil caps pdf: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(nil, media.KindText); d != MediaAttempt {
		t.Errorf("nil caps text: got %s, want %s", d, MediaAttempt)
	}

	// Zero-valued capabilities → attempt everything
	var zeroCaps goai_provider.ModelCapabilities
	if d := MediaDecisionFor(&zeroCaps, media.KindImage); d != MediaAttempt {
		t.Errorf("zero caps image: got %s, want %s", d, MediaAttempt)
	}

	// Known capabilities: text+image+pdf supported
	fullCaps := &goai_provider.ModelCapabilities{
		Attachment:      true,
		InputModalities: goai_provider.ModalitySet{
			Text:  true,
			Image: true,
			PDF:   true,
		},
	}
	if d := MediaDecisionFor(fullCaps, media.KindImage); d != MediaAllow {
		t.Errorf("full caps image: got %s, want %s", d, MediaAllow)
	}
	if d := MediaDecisionFor(fullCaps, media.KindPDF); d != MediaAllow {
		t.Errorf("full caps pdf: got %s, want %s", d, MediaAllow)
	}
	if d := MediaDecisionFor(fullCaps, media.KindText); d != MediaAllow {
		t.Errorf("full caps text: got %s, want %s", d, MediaAllow)
	}
	if d := MediaDecisionFor(fullCaps, media.KindAudio); d != MediaAttempt {
		t.Errorf("full caps audio (unknown kind): got %s, want %s", d, MediaAttempt)
	}

	// Text-only model (no Attachment flag): attempt delivery optimistically
	// (covers vllm/ollama/litellm compat providers)
	textOnlyCaps := &goai_provider.ModelCapabilities{
		InputModalities: goai_provider.ModalitySet{Text: true},
	}
	if d := MediaDecisionFor(textOnlyCaps, media.KindImage); d != MediaAttempt {
		t.Errorf("text-only caps image: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(textOnlyCaps, media.KindPDF); d != MediaAttempt {
		t.Errorf("text-only caps pdf: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(textOnlyCaps, media.KindText); d != MediaAttempt {
		t.Errorf("text-only caps text: got %s, want %s", d, MediaAttempt)
	}
}

func TestIsMediaRejection(t *testing.T) {

	mediaErrors := []string{
		"image_input is not supported for this model",
		"this model does not support images",
		"model does not support multimodal input",
		"vision is not available",
		"file_data is not supported",
		"pdf is not supported for this model",
		"input modality not supported",
		"unsupported modality: image",
		"content parts with data:image are not supported",
	}

	for _, msg := range mediaErrors {
		err := errors.New(msg)
		if !isMediaRejection(err) {
			t.Errorf("expected media rejection for: %q", msg)
		}
	}

	// Nil error should not be classified
	if isMediaRejection(nil) {
		t.Error("nil error should not be media rejection")
	}

	nonMediaErrors := []string{
		"401 unauthorized",
		"authentication failed",
		"invalid_api_key",
		"rate limit exceeded",
		"too many requests",
		"server error: 500",
		"connection refused",
		"context deadline exceeded",
		"quota exceeded",
		"safety filter triggered",
		"content_filter blocked the response",
		"the model violates usage policy",
		"prompt is too long",
		"exceeds the context window",
		"network error: dial tcp",
	}

	for _, msg := range nonMediaErrors {
		err := errors.New(msg)
		if isMediaRejection(err) {
			t.Errorf("should NOT be media rejection for: %q", msg)
		}
	}

	// Wrapped APIError with media rejection message
	apiErr := &goai.APIError{
		Message:    "image_input is not supported for this model",
		StatusCode: 400,
	}
	if !isMediaRejection(apiErr) {
		t.Error("wrapped APIError with media message should be media rejection")
	}

	// Wrapped APIError with non-media message should not be classified
	apiErr2 := &goai.APIError{
		Message:    "authentication failed",
		StatusCode: 401,
	}
	if isMediaRejection(apiErr2) {
		t.Error("APIError with auth message should NOT be media rejection")
	}

	// Wrapped error: errors.Wrap-style
	wrapped := errors.New("wrapped: " + "model does not support images")
	if !isMediaRejection(wrapped) {
		t.Error("wrapped error with media message should be media rejection")
	}

	// Custom error wrapping a goai.APIError
	type wrappedAPIError struct {
		wrapped error
	}
	w := wrappedAPIError{wrapped: &goai.APIError{Message: "image not supported", StatusCode: 400}}
	// Our asAPIError only unwraps via standard Unwrap, so this won't match
	// unless we implement Unwrap. Test the direct case.
	_ = w // suppress unused
}

func TestIsMediaRejectionCaseInsensitive(t *testing.T) {
	err := errors.New("IMAGE_INPUT is NOT SUPPORTED")
	if !isMediaRejection(err) {
		t.Error("should handle case-insensitive match")
	}
}

func TestIsNonMediaError(t *testing.T) {
	// Ensure non-media patterns are correctly identified
	if !isNonMediaError("401 unauthorized") {
		t.Error("expected non-media error for 401")
	}
	if !isNonMediaError("rate limit exceeded") {
		t.Error("expected non-media error for rate limit")
	}
	if !isNonMediaError("content_filter blocked") {
		t.Error("expected non-media error for content_filter")
	}
	if !isNonMediaError("safety filter triggered") {
		t.Error("expected non-media error for safety")
	}

	// Media errors should NOT be classified as non-media
	if isNonMediaError("image_input is not supported") {
		t.Error("media error should not be non-media")
	}
}

func TestMediaDecisions(t *testing.T) {
	// Verify constants are distinct strings
	if MediaAllow == MediaOmit {
		t.Error("MediaAllow and MediaOmit should be different")
	}
	if MediaAllow == MediaAttempt {
		t.Error("MediaAllow and MediaAttempt should be different")
	}
	if MediaOmit == MediaAttempt {
		t.Error("MediaOmit and MediaAttempt should be different")
	}

	// Verify string values
	if string(MediaAllow) != "allow" {
		t.Errorf("MediaAllow string: got %q, want %q", MediaAllow, "allow")
	}
	if string(MediaOmit) != "omit" {
		t.Errorf("MediaOmit string: got %q, want %q", MediaOmit, "omit")
	}
	if string(MediaAttempt) != "attempt" {
		t.Errorf("MediaAttempt string: got %q, want %q", MediaAttempt, "attempt")
	}
}

// Test that the media rejection classifier does not match on substrings
// that appear in unrelated error messages.
func TestIsMediaRejectionNoFalsePositives(t *testing.T) {

	// These contain words like "image" or "file" but in a non-media-rejection context
	falsePositives := []string{
		"the image generation tool is not available",
		"file not found: /tmp/test.txt",
		"media directory does not exist",
		"attachment image processing failed due to network error",
	}

	for _, msg := range falsePositives {
		err := errors.New(msg)
		if isMediaRejection(err) {
			t.Errorf("should NOT be media rejection for: %q", msg)
		}
	}
}

// Test asAPIError helper
func TestAsAPIError(t *testing.T) {
	// Direct APIError
	apiErr := &goai.APIError{Message: "test"}
	target := new(*goai.APIError)
	if !asAPIError(apiErr, target) || *target != apiErr {
		t.Error("direct APIError should match")
	}

	// Non-APIError
	plainErr := errors.New("plain error")
	target = new(*goai.APIError)
	if asAPIError(plainErr, target) || *target != nil {
		t.Error("plain error should not match as APIError")
	}

	// Nil error
	target = new(*goai.APIError)
	if asAPIError(nil, target) || *target != nil {
		t.Error("nil error should not match as APIError")
	}
}

// Test that media rejection on wrapped APIError works correctly
func TestIsMediaRejectionWrappedAPIError(t *testing.T) {

	// fmt.Errorf wrapping
	apiErr := &goai.APIError{Message: "image_input is not supported for this model", StatusCode: 400}
	wrapped := fmtWrapped{err: apiErr}
	if !isMediaRejection(wrapped) {
		t.Error("wrapped APIError with media message should be media rejection")
	}

	// Wrapped with non-media message
	apiErr2 := &goai.APIError{Message: "quota exceeded", StatusCode: 429}
	wrapped2 := fmtWrapped{err: apiErr2}
	if isMediaRejection(wrapped2) {
		t.Error("wrapped APIError with quota message should NOT be media rejection")
	}
}

// fmtWrapped is a simple error wrapper that implements Unwrap for testing.
type fmtWrapped struct {
	err error
	prefix string
}

func (w fmtWrapped) Error() string {
	return w.prefix + ": " + w.err.Error()
}

func (w fmtWrapped) Unwrap() error {
	return w.err
}

// Test that the media policy correctly handles the "attempt" case for
// compat (unknown) models — which have only text capability.
func TestMediaPolicyCompatModel(t *testing.T) {

	// Compat provider caps: text only, no image/pdf
	compatCaps := &goai_provider.ModelCapabilities{
		Temperature: true,
		ToolCall:    true,
		InputModalities: goai_provider.ModalitySet{
			Text: true,
		},
		OutputModalities: goai_provider.ModalitySet{
			Text: true,
		},
	}

	// With compat caps (no Attachment flag), attempt delivery optimistically
	if d := MediaDecisionFor(compatCaps, media.KindImage); d != MediaAttempt {
		t.Errorf("compat caps image: got %s, want %s", d, MediaAttempt)
	}
	if d := MediaDecisionFor(compatCaps, media.KindText); d != MediaAttempt {
		t.Errorf("compat caps text: got %s, want %s", d, MediaAttempt)
	}
}

// Test that unknown provider errors (random strings) are not misclassified.
func TestIsMediaRejectionUnknownErrors(t *testing.T) {

	unknownErrors := []string{
		"something went wrong",
		"internal server error",
		"unexpected EOF",
		"bad gateway",
		"gateway timeout",
		"request timeout",
		"model not found",
		"invalid request",
		"the model is currently unavailable",
	}

	for _, msg := range unknownErrors {
		err := errors.New(msg)
		if isMediaRejection(err) {
			t.Errorf("unknown error should not be media rejection: %q", msg)
		}
	}
}

// Ensure strings.ToLower is applied consistently in the classifier.
func TestIsMediaRejectionMixedCase(t *testing.T) {

	mixedCase := []string{
		"IMAGE_INPUT Is Not Supported",
		"Image Input is not supported for this model",
		"Model DOES NOT SUPPORT IMAGES",
	}

	for _, msg := range mixedCase {
		err := errors.New(msg)
		if !isMediaRejection(err) {
			t.Errorf("expected media rejection (mixed case): %q", msg)
		}
	}
}
