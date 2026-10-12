package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/webhook"
)

func TestMediaManager_FileLimits(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "media-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		BotMediaTempDir:    tempDir,
		BotMaxImageMB:      1,
		BotMaxAudioMB:      1,
		BotMaxAudioMinutes: 5,
		BotMaxVideoMB:      1,
		BotMaxVideoSeconds: 90,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create a dummy image file of 2MB (exceeds 1MB limit)
	largeFile := filepath.Join(tempDir, "large.jpg")
	dummyBytes := make([]byte, 2*1024*1024)
	if err := os.WriteFile(largeFile, dummyBytes, 0644); err != nil {
		t.Fatal(err)
	}

	msg := &webhook.NormalizedMessage{
		Type:          webhook.MediaTypeImage,
		MediaPath:     largeFile,
		MediaMimeType: "image/jpeg",
	}

	_, err = mgr.PrepareMedia(context.Background(), msg)
	if err == nil {
		t.Fatal("Expected error for oversized media, got nil")
	}
}
