package webhook

import (
	"testing"
)

func TestVerifyHMAC(t *testing.T) {
	secret := "super-secret-key-12345"
	payload := []byte(`{"event":"message","id":"ABC123"}`)

	validSig := ComputeHMAC(payload, secret)

	// 1. Valid signature
	if err := VerifyHMAC(payload, validSig, secret); err != nil {
		t.Fatalf("Expected valid HMAC to pass, got: %v", err)
	}

	// 2. Valid signature with "sha256=" prefix
	if err := VerifyHMAC(payload, "sha256="+validSig, secret); err != nil {
		t.Fatalf("Expected prefixed HMAC to pass, got: %v", err)
	}

	// 3. Invalid signature
	if err := VerifyHMAC(payload, "deadbeef1234", secret); err == nil {
		t.Fatal("Expected invalid signature to fail")
	}

	// 4. Missing signature
	if err := VerifyHMAC(payload, "", secret); err != ErrMissingSignature {
		t.Fatalf("Expected ErrMissingSignature, got: %v", err)
	}

	// 5. Unconfigured secret allows passthrough
	if err := VerifyHMAC(payload, "", ""); err != nil {
		t.Fatalf("Expected empty secret to pass, got: %v", err)
	}
}
