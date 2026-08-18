package chat

import (
	"strings"

	"squid-os/internal/media"

	goai "github.com/zendev-sh/goai"
	goai_provider "github.com/zendev-sh/goai/provider"
)

// MediaDecision classifies how an attachment should be treated for a given model.
type MediaDecision string

const (
	// MediaAllow means the model explicitly supports this modality.
	MediaAllow MediaDecision = "allow"
	// MediaOmit means the model explicitly does not support this modality.
	MediaOmit MediaDecision = "omit"
	// MediaAttempt means capabilities are unknown or missing — attempt delivery optimistically.
	MediaAttempt MediaDecision = "attempt"
)

// MediaDecisionFor returns the media decision for the given kind based on model
// capabilities. It is a pure function of (caps, kind) — no injected policy.
//
// Rules:
//   - If caps is nil or zero-valued, all kinds are attempted (unknown model).
//   - If the modality is explicitly supported in caps.InputModalities, allow.
//   - If the modality is explicitly absent in caps.InputModalities, omit.
//   - Text is always allowed (no modality flag needed).
func MediaDecisionFor(caps *goai_provider.ModelCapabilities, kind media.Kind) MediaDecision {
	if caps == nil || !caps.Attachment {
		// Caps are nil or the provider doesn't declare attachment support.
		// This covers zero-value caps and compat/local providers (vllm, ollama,
		// litellm) that only declare text. We don't know if the specific model
		// supports vision — attempt delivery optimistically.
		return MediaAttempt
	}

	switch kind {
	case media.KindText:
		return MediaAllow
	case media.KindImage:
		if caps.InputModalities.Image {
			return MediaAllow
		}
		return MediaOmit
	case media.KindPDF, media.KindFile:
		if caps.InputModalities.PDF {
			return MediaAllow
		}
		return MediaOmit
	default:
		// Unknown kind (audio, video, etc.) — attempt delivery.
		return MediaAttempt
	}
}

// isMediaRejection returns true if the error is a provider rejection caused by
// unsupported media content. It does NOT match authentication, network, safety,
// quota, or unrelated errors.
func isMediaRejection(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	// Media-specific rejection patterns from various providers.
	// These are designed to match actual provider error messages about
	// unsupported media, not general mentions of "media" or "image".
	mediaPatterns := []string{
		"image_input",
		"image input is not supported",
		"image_url",
		"image_url is not supported",
		"does not support image",
		"does not support images",
		"multimodal input",
		"multimodal content",
		"vision is not available",
		"vision is not supported",
		"file_input",
		"file_input is not supported",
		"file_data",
		"file_data is not supported",
		"pdf is not supported",
		"pdf files are not supported",
		"content parts with",
		"input modality",
		"unsupported modality",
		"base64 image",
		"data:image",
		"data_uri",
		"input_image",
		"image parts",
	}

	for _, pat := range mediaPatterns {
		if strings.Contains(msg, pat) {
			// Exclude non-media errors that might coincidentally contain
			// some of these words in a different context.
			if isNonMediaError(msg) {
				return false
			}
			return true
		}
	}

	// Check for GoAI APIError with known media-rejection status codes.
	var apiErr *goai.APIError
	if asAPIError(err, &apiErr) {
		// 400 Bad Request with a media-related message
		if apiErr.StatusCode == 400 {
			for _, pat := range mediaPatterns {
				if strings.Contains(strings.ToLower(apiErr.Message), pat) {
					return true
				}
			}
		}
	}

	return false
}

// isNonMediaError returns true if the message clearly indicates a non-media error
// even if it contains words that overlap with media patterns.
func isNonMediaError(msg string) bool {
	nonMediaPatterns := []string{
		"authentication",
		"unauthorized",
		"401",
		"403",
		"forbidden",
		"quota",
		"rate limit",
		"rate_limit",
		"rate limit",
		"429",
		"too many requests",
		"overloaded",
		"server error",
		"500",
		"503",
		"network",
		"connection refused",
		"dial tcp",
		"context deadline",
		"context canceled",
		"safety",
		"content_filter",
		"content filter",
		"blocked",
		"violates",
	}

	for _, pat := range nonMediaPatterns {
		if strings.Contains(msg, pat) {
			return true
		}
	}

	return false
}

// asAPIError attempts to unwrap err into a *goai.APIError.
func asAPIError(err error, target **goai.APIError) bool {
	for err != nil {
		if ae, ok := err.(*goai.APIError); ok {
			*target = ae
			return true
		}
		// Unwrap using the standard errors interface.
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}
