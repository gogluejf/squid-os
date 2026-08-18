package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"squid-os/internal/config"
	"squid-os/internal/media"

	goai_provider "github.com/zendev-sh/goai/provider"
)

func TestResolveImagePart(t *testing.T) {
	imgData := []byte("fake image data")

	part := resolveImagePart(media.Attachment{MIME: "image/png"}, imgData)
	if part.Type != goai_provider.PartImage {
		t.Errorf("type: got %v, want %v", part.Type, goai_provider.PartImage)
	}
	if !strings.HasPrefix(part.URL, "data:image/png;base64,") {
		t.Errorf("URL should be data URI, got: %s...", part.URL[:min(40, len(part.URL))])
	}
	if part.MediaType != "image/png" {
		t.Errorf("MediaType: got %v, want image/png", part.MediaType)
	}
	if part.Detail != "auto" {
		t.Errorf("Detail: got %v, want auto", part.Detail)
	}
}

func TestResolveFilePart(t *testing.T) {
	data := []byte("pdf binary content")
	part := resolveFilePart(media.Attachment{MIME: "application/pdf", DisplayName: "doc.pdf"}, data)
	if part.Type != goai_provider.PartFile {
		t.Errorf("type: got %v, want %v", part.Type, goai_provider.PartFile)
	}
	if !strings.HasPrefix(part.URL, "data:application/pdf;base64,") {
		t.Errorf("URL should be data URI, got: %s...", part.URL[:min(50, len(part.URL))])
	}
	if part.MediaType != "application/pdf" {
		t.Errorf("MediaType: got %v, want application/pdf", part.MediaType)
	}
	if part.Filename != "doc.pdf" {
		t.Errorf("Filename: got %v, want doc.pdf", part.Filename)
	}
}

func TestResolveTextPartNoTruncation(t *testing.T) {
	smallText := []byte("short text content")
	part := resolveTextPart(media.Attachment{DisplayName: "note.txt", MIME: "text/plain"}, smallText)
	if part.Type != goai_provider.PartText {
		t.Errorf("type: got %v, want %v", part.Type, goai_provider.PartText)
	}
	if !strings.Contains(part.Text, "--- inline: note.txt (text/plain) ---") {
		t.Error("missing source marker")
	}
	if !strings.Contains(part.Text, "--- end inline ---") {
		t.Error("missing end marker")
	}
	if strings.Contains(part.Text, "truncated") {
		t.Error("small text should not be truncated")
	}
}

func TestResolveTextPartWithTruncation(t *testing.T) {
	largeText := make([]byte, maxInlineTextBytes+1000)
	for i := range largeText {
		largeText[i] = 'x'
	}
	part := resolveTextPart(media.Attachment{DisplayName: "big.log", MIME: "text/plain"}, largeText)
	if part.Type != goai_provider.PartText {
		t.Errorf("type: got %v, want %v", part.Type, goai_provider.PartText)
	}
	if !strings.Contains(part.Text, "[truncated:") {
		t.Error("large text should contain truncation marker")
	}
	if !strings.Contains(part.Text, "bytes omitted") {
		t.Error("truncation marker should mention omitted bytes")
	}
}

func TestResolveAttachmentToPartImage(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.png")
	if err := os.WriteFile(imgPath, []byte("fake png"), 0644); err != nil {
		t.Fatal(err)
	}

	a := media.Attachment{ID: "img1", FileName: "test.png", MIME: "image/png", Kind: media.KindImage, Size: 8}
	part, err := resolveAttachmentToPart(a, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.Type != goai_provider.PartImage {
		t.Errorf("part type: got %v, want PartImage", part.Type)
	}
}

func TestResolveAttachmentToPartMissingFile(t *testing.T) {
	a := media.Attachment{ID: "gone", FileName: "gone.png", MIME: "image/png", Kind: media.KindImage, Size: 0}
	_, err := resolveAttachmentToPart(a, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "gone.png") {
		t.Errorf("error should mention the filename: %v", err)
	}
}

func TestBuildUserMessagePartsNoAttachments(t *testing.T) {
	msg := config.Message{ID: "u1", Role: config.RoleUser, Text: "hello world"}
	parts := buildUserMessageParts(msg, nil, "", nil)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != goai_provider.PartText {
		t.Errorf("type: got %v, want %v", parts[0].Type, goai_provider.PartText)
	}
	if parts[0].Text != "hello world" {
		t.Errorf("text: got %q, want %q", parts[0].Text, "hello world")
	}
}

func TestBuildUserMessagePartsRetainsCanonicalText(t *testing.T) {
	msg := config.Message{ID: "u1", Role: config.RoleUser, Text: "analyze @file:img1"}
	parts := buildUserMessageParts(msg, nil, "", nil)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (no attachments), got %d", len(parts))
	}
	if !strings.Contains(parts[0].Text, "@file:img1") {
		t.Error("canonical @file: reference should be retained in text")
	}
}

func TestBuildUserMessagePartsWithAttachment(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "photo.png")
	if err := os.WriteFile(imgPath, []byte("png data"), 0644); err != nil {
		t.Fatal(err)
	}

	attachments := []media.Attachment{
		{ID: "img1", FileName: "photo.png", DisplayName: "photo.png", MIME: "image/png", Kind: media.KindImage, Size: 8},
	}

	msg := config.Message{
		ID:    "u1",
		Role:  config.RoleUser,
		Text:  "Analyze @file:photo.png",
		Attachments: []config.AttachmentRef{
			{File: "photo.png", Tokens: 2},
		},
	}

	parts := buildUserMessageParts(msg, attachments, tmpDir, nil)
	// Should have text part + image part
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d", len(parts))
	}
	if parts[0].Type != goai_provider.PartText {
		t.Errorf("first part should be text, got %v", parts[0].Type)
	}
	if parts[1].Type != goai_provider.PartImage {
		t.Errorf("second part should be image, got %v", parts[1].Type)
	}
}

func TestEstimateAttachmentTokens(t *testing.T) {
	// Image: ~1 token per 4 bytes
	tokens := EstimateAttachmentTokens(media.Attachment{Kind: media.KindImage, Size: 4000})
	if tokens != 1000 {
		t.Errorf("image tokens: got %d, want 1000", tokens)
	}

	// PDF: ~1 token per 4 bytes
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: media.KindPDF, Size: 8000})
	if tokens != 2000 {
		t.Errorf("pdf tokens: got %d, want 2000", tokens)
	}

	// Text: standard approximation
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: media.KindText, Size: 11})
	if tokens == 0 {
		t.Error("text tokens should be > 0")
	}

	// Unknown kind: conservative estimate (~1 per 4 bytes)
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: "unknown", Size: 4000})
	if tokens != 1000 {
		t.Errorf("unknown kind tokens: got %d, want 1000", tokens)
	}

	// Audio: ~1 token per 4 bytes
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: media.KindAudio, Size: 8000})
	if tokens != 2000 {
		t.Errorf("audio tokens: got %d, want 2000", tokens)
	}

	// Video: ~1 token per 4 bytes
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: media.KindVideo, Size: 12000})
	if tokens != 3000 {
		t.Errorf("video tokens: got %d, want 3000", tokens)
	}

	// Large text attachment: bounded by maxInlineTextBytes
	largeTextSize := int64(maxInlineTextBytes) + 10000
	tokens = EstimateAttachmentTokens(media.Attachment{Kind: media.KindText, Size: largeTextSize})
	expected := CountTokensApproxInt(int(maxInlineTextBytes))
	if tokens != expected {
		t.Errorf("large text tokens: got %d, want %d (bounded by maxInlineTextBytes)", tokens, expected)
	}
}

// TestBuildContextWithStoredRefs verifies the full build path reads stored
// AttachmentRefs from messages rather than scanning text.
func TestBuildContextWithStoredRefs(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("fake png bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	attachments := []media.Attachment{
		{ID: "a1", FileName: "chart.png", DisplayName: "chart.png", MIME: "image/png", Kind: media.KindImage, Size: 14, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}

	msgs := []config.Message{
		{
			ID: "u1", Role: config.RoleUser,
			Text: "Look at @file:chart.png",
			Attachments: []config.AttachmentRef{
				{File: "chart.png", Tokens: 4},
			},
		},
		{ID: "a1", Role: config.RoleAssistant, Text: "Here is the chart."},
	}

	ctx := BuildContext(msgs, false, tmpDir, attachments, nil)
	if len(ctx.Messages) != 2 {
		t.Fatalf("expected 2 provider messages, got %d", len(ctx.Messages))
	}
	// User message should have text + image parts
	userMsg := ctx.Messages[0]
	if userMsg.Role != goai_provider.RoleUser {
		t.Fatalf("first message role: got %v, want user", userMsg.Role)
	}
	foundImage := false
	for _, p := range userMsg.Content {
		if p.Type == goai_provider.PartImage {
			foundImage = true
		}
	}
	if !foundImage {
		t.Error("expected image part in user message")
	}
}
