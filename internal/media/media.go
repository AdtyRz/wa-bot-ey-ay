package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/webhook"
)

var (
	ErrFileTooLarge       = errors.New("file exceeds maximum allowed size")
	ErrDurationTooLong    = errors.New("media duration exceeds maximum allowed length")
	ErrUnsupportedMime    = errors.New("unsupported media MIME type")
	ErrInvalidPath        = errors.New("invalid or unsafe media path")
	ErrDisallowedURL      = errors.New("disallowed or invalid media URL")
	ErrDownloadFailed     = errors.New("failed to download media")
	ErrExtractionFailed   = errors.New("failed to extract media frame/audio")
)

type Manager struct {
	cfg     *config.Config
	tempDir string
}

func NewManager(cfg *config.Config) (*Manager, error) {
	tempDir := cfg.BotMediaTempDir
	if tempDir == "" {
		tempDir = "/tmp/temanwa-media"
	}
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create media temp dir: %w", err)
	}
	return &Manager{cfg: cfg, tempDir: tempDir}, nil
}

type ProcessedMedia struct {
	LocalPath    string
	MimeType     string
	Size         int64
	DurationSec  int
	Frames       []string // Path to extracted frame(s) if video/sticker
	ExtractedText string  // Text if document/PDF
}

// PrepareMedia downloads/validates and prepares local media for AI processing
func (m *Manager) PrepareMedia(ctx context.Context, msg *webhook.NormalizedMessage) (*ProcessedMedia, error) {
	var localFile string
	var err error

	// 1. Resolve local path or download from URL
	if msg.MediaPath != "" {
		validatedPath, err := m.validateLocalPath(msg.MediaPath)
		if err != nil {
			return nil, err
		}
		// Copy local file to temp directory so Cleanup does not remove the original file
		ext := filepath.Ext(validatedPath)
		tmpFile, err := os.CreateTemp(m.tempDir, "media-*"+ext)
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file for media: %w", err)
		}
		defer tmpFile.Close()

		srcFile, err := os.Open(validatedPath)
		if err != nil {
			// If host permission denied, read via docker exec/cp since container created it
			relToGowa := strings.TrimPrefix(validatedPath, "/home/adtyaa/gowa_data/")
			cmd := exec.CommandContext(ctx, "docker", "exec", "gowa", "cat", "/app/"+relToGowa)
			data, cmdErr := cmd.Output()
			if cmdErr != nil {
				_ = os.Remove(tmpFile.Name())
				return nil, fmt.Errorf("failed to open source media: %w", err)
			}
			if _, wErr := tmpFile.Write(data); wErr != nil {
				_ = os.Remove(tmpFile.Name())
				return nil, fmt.Errorf("failed to write media to temp: %w", wErr)
			}
		} else {
			defer srcFile.Close()
			if _, err := io.Copy(tmpFile, srcFile); err != nil {
				_ = os.Remove(tmpFile.Name())
				return nil, fmt.Errorf("failed to copy media to temp: %w", err)
			}
		}
		localFile = tmpFile.Name()
	} else if msg.MediaURL != "" {
		localFile, err = m.downloadMedia(ctx, msg.MediaURL, msg.Type)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("no media URL or path provided")
	}

	// 2. Inspect file size
	fi, err := os.Stat(localFile)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	size := fi.Size()

	// 3. Detect MIME type using magic bytes
	detectedMime, err := detectMimeType(localFile)
	if err != nil {
		detectedMime = msg.MediaMimeType
	}
	if detectedMime == "" {
		detectedMime = msg.MediaMimeType
	}

	// 4. Validate by media type
	switch msg.Type {
	case webhook.MediaTypeImage:
		maxBytes := m.cfg.BotMaxImageMB * 1024 * 1024
		if size > maxBytes {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d MB", ErrFileTooLarge, m.cfg.BotMaxImageMB)
		}
		if !strings.HasPrefix(detectedMime, "image/") {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: got %s", ErrUnsupportedMime, detectedMime)
		}

	case webhook.MediaTypeAudio:
		maxBytes := m.cfg.BotMaxAudioMB * 1024 * 1024
		if size > maxBytes {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d MB", ErrFileTooLarge, m.cfg.BotMaxAudioMB)
		}
		if msg.MediaDuration > m.cfg.BotMaxAudioMinutes*60 {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d minutes", ErrDurationTooLong, m.cfg.BotMaxAudioMinutes)
		}

	case webhook.MediaTypeVideo:
		maxBytes := m.cfg.BotMaxVideoMB * 1024 * 1024
		if size > maxBytes {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d MB", ErrFileTooLarge, m.cfg.BotMaxVideoMB)
		}
		if msg.MediaDuration > m.cfg.BotMaxVideoSeconds {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d seconds", ErrDurationTooLong, m.cfg.BotMaxVideoSeconds)
		}

	case webhook.MediaTypeSticker:
		var maxBytes int64 = 5 * 1024 * 1024 // 5 MB for stickers
		if size > maxBytes {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: sticker max 5 MB", ErrFileTooLarge)
		}

	case webhook.MediaTypeDocument:
		maxBytes := m.cfg.BotMaxDocumentMB * 1024 * 1024
		if size > maxBytes {
			_ = os.Remove(localFile)
			return nil, fmt.Errorf("%w: max %d MB", ErrFileTooLarge, m.cfg.BotMaxDocumentMB)
		}
	}

	res := &ProcessedMedia{
		LocalPath:   localFile,
		MimeType:    detectedMime,
		Size:        size,
		DurationSec: msg.MediaDuration,
	}

	// 5. Handle Video / Animated Sticker frame extraction via FFmpeg if available
	if msg.Type == webhook.MediaTypeVideo || (msg.Type == webhook.MediaTypeSticker && msg.IsAnimated) {
		framePath, err := m.extractKeyFrame(ctx, localFile)
		if err == nil && framePath != "" {
			res.Frames = append(res.Frames, framePath)
		}
	}

	return res, nil
}

func (m *Manager) validateLocalPath(p string) (string, error) {
	clean := filepath.Clean(p)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", ErrInvalidPath
	}
	// Check if path exists
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("local file does not exist: %w", err)
	}
	return abs, nil
}

func (m *Manager) downloadMedia(ctx context.Context, rawURL string, mType webhook.MediaType) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrDisallowedURL
	}

	// Determine max size
	var maxBytes int64 = 25 * 1024 * 1024
	switch mType {
	case webhook.MediaTypeImage:
		maxBytes = m.cfg.BotMaxImageMB * 1024 * 1024
	case webhook.MediaTypeAudio:
		maxBytes = m.cfg.BotMaxAudioMB * 1024 * 1024
	case webhook.MediaTypeVideo:
		maxBytes = m.cfg.BotMaxVideoMB * 1024 * 1024
	case webhook.MediaTypeDocument:
		maxBytes = m.cfg.BotMaxDocumentMB * 1024 * 1024
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrDownloadFailed, resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp(m.tempDir, "media-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Limit reader to maxBytes + 1 to detect overflow
	limited := io.LimitReader(resp.Body, maxBytes+1)
	written, err := io.Copy(tmpFile, limited)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", err
	}
	if written > maxBytes {
		_ = os.Remove(tmpFile.Name())
		return "", ErrFileTooLarge
	}

	return tmpFile.Name(), nil
}

func (m *Manager) extractKeyFrame(ctx context.Context, mediaPath string) (string, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", err
	}

	outFrame := filepath.Join(m.tempDir, fmt.Sprintf("frame-%d.jpg", time.Now().UnixNano()))
	// ffmpeg -ss 00:00:01 -i <file> -vframes 1 -q:v 2 <outFrame>
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-ss", "00:00:01", "-i", mediaPath, "-vframes", "1", "-q:v", "2", outFrame)
	if err := cmd.Run(); err != nil {
		// Fallback to first frame at 0s if 1s failed
		cmd2 := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", mediaPath, "-vframes", "1", "-q:v", "2", outFrame)
		if err2 := cmd2.Run(); err2 != nil {
			return "", ErrExtractionFailed
		}
	}

	if _, err := os.Stat(outFrame); err != nil {
		return "", ErrExtractionFailed
	}

	return outFrame, nil
}

func (m *Manager) Cleanup(files ...string) {
	for _, f := range files {
		if f != "" {
			_ = os.Remove(f)
		}
	}
}

// StartTTLCleanup runs a background goroutine to clean old files in tempDir
func (m *Manager) StartTTLCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.purgeOldFiles()
			}
		}
	}()
}

func (m *Manager) purgeOldFiles() {
	entries, err := os.ReadDir(m.tempDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-m.cfg.BotMediaTempTTL)
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(m.tempDir, entry.Name()))
		}
	}
}

func detectMimeType(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}

	contentType := http.DetectContentType(buf[:n])
	return contentType, nil
}
