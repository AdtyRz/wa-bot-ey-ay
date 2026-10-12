package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrMissingSecret    = errors.New("webhook secret not configured")
	ErrMissingSignature = errors.New("missing webhook signature")
	ErrInvalidSignature = errors.New("invalid webhook signature")
)

// VerifyHMAC validates the HMAC-SHA256 signature of the payload using constant-time comparison
func VerifyHMAC(payload []byte, signatureHeader, secret string) error {
	if secret == "" {
		// If secret is not configured, pass validation or treat as unverified depending on policy
		// PRD states: verify signature when configured
		return nil
	}

	if signatureHeader == "" {
		return ErrMissingSignature
	}

	cleanSig := strings.TrimSpace(signatureHeader)
	// Handle headers with "sha256=" prefix if present
	if strings.HasPrefix(strings.ToLower(cleanSig), "sha256=") {
		cleanSig = cleanSig[7:]
	}

	sigBytes, err := hex.DecodeString(cleanSig)
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expectedMAC) {
		return ErrInvalidSignature
	}

	return nil
}

// ComputeHMAC computes hex-encoded HMAC-SHA256 for a given payload and secret (useful for testing)
func ComputeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
