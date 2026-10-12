package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"temanwa-ai/internal/config"
)

var (
	ErrQuotaExceeded    = errors.New("gemini api quota exceeded")
	ErrRateLimited      = errors.New("gemini api rate limited")
	ErrInvalidAPIKey    = errors.New("missing or invalid gemini api key")
	ErrModelUnavailable = errors.New("gemini model unavailable")
)

type AIClient interface {
	GenerateText(ctx context.Context, systemInstruction, prompt string) (string, error)
	GenerateMultimodal(ctx context.Context, systemInstruction, prompt string, mediaBytes []byte, mimeType string) (string, error)
	CheckQuota(userJID string) bool
}

type GeminiClient struct {
	cfg        *config.Config
	httpClient *http.Client
	sem        chan struct{} // bounded concurrency semaphore

	// In-memory rate limiting per user: userJID -> []timestamps
	mu          sync.Mutex
	userHistory map[string][]time.Time
}

func NewGeminiClient(cfg *config.Config) *GeminiClient {
	maxConc := cfg.AIMaxConcurrentRequests
	if maxConc <= 0 {
		maxConc = 5
	}

	dialer := &net.Dialer{
		Timeout:       5 * time.Second,
		KeepAlive:     30 * time.Second,
		FallbackDelay: 50 * time.Millisecond,
	}

	t := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 30,
		IdleConnTimeout:     90 * time.Second,
	}

	return &GeminiClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: t,
		},
		sem:         make(chan struct{}, maxConc),
		userHistory: make(map[string][]time.Time),
	}
}

// CheckQuota enforces per-user request limits per hour
func (c *GeminiClient) CheckQuota(userJID string) bool {
	if c.cfg.AIMaxRequestsPerUserHr <= 0 {
		return true
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	timestamps := c.userHistory[userJID]
	valid := make([]time.Time, 0, len(timestamps))
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= c.cfg.AIMaxRequestsPerUserHr {
		c.userHistory[userJID] = valid
		return false
	}

	valid = append(valid, now)
	c.userHistory[userJID] = valid
	return true
}

type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

type ThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type GenerationConfig struct {
	Temperature     float64         `json:"temperature"`
	MaxOutputTokens int             `json:"maxOutputTokens"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

type GenerateContentRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
}

type GenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (c *GeminiClient) GenerateText(ctx context.Context, systemInstruction, prompt string) (string, error) {
	return c.callGemini(ctx, systemInstruction, []Part{{Text: prompt}})
}

func (c *GeminiClient) GenerateMultimodal(ctx context.Context, systemInstruction, prompt string, mediaBytes []byte, mimeType string) (string, error) {
	parts := []Part{
		{
			InlineData: &InlineData{
				MimeType: mimeType,
				Data:     base64.StdEncoding.EncodeToString(mediaBytes),
			},
		},
		{Text: prompt},
	}
	return c.callGemini(ctx, systemInstruction, parts)
}

func (c *GeminiClient) callGemini(ctx context.Context, sysInst string, parts []Part) (string, error) {
	if c.cfg.GeminiAPIKey == "" {
		return "", ErrInvalidAPIKey
	}

	// Concurrency limiter
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	model := c.cfg.GeminiModel
	if model == "" {
		model = "gemini-2.5-flash"
	}

	reqBody := GenerateContentRequest{
		Contents: []Content{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		GenerationConfig: &GenerationConfig{
			Temperature:     c.cfg.GeminiTemperature,
			MaxOutputTokens: c.cfg.GeminiMaxOutputTokens,
			ThinkingConfig:  &ThinkingConfig{ThinkingBudget: 0},
		},
	}

	if sysInst != "" {
		reqBody.SystemInstruction = &SystemInstruction{
			Parts: []Part{{Text: sysInst}},
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, c.cfg.GeminiAPIKey)

	// Retry loop for transient errors
	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt*500) * time.Millisecond):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			bodyStr := strings.ToLower(string(respBody))
			if strings.Contains(bodyStr, "quota") || strings.Contains(bodyStr, "resource_exhausted") {
				lastErr = ErrQuotaExceeded
			} else {
				lastErr = ErrRateLimited
			}
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("%w: %s", ErrModelUnavailable, model)
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("gemini server error (%d)", resp.StatusCode)
			continue
		}

		var geminiResp GenerateContentResponse
		if err := json.Unmarshal(respBody, &geminiResp); err != nil {
			return "", fmt.Errorf("failed to parse gemini response: %w", err)
		}

		if geminiResp.Error != nil {
			errLower := strings.ToLower(geminiResp.Error.Message)
			if strings.Contains(errLower, "quota") || strings.Contains(errLower, "resource_exhausted") {
				return "", ErrQuotaExceeded
			}
			if geminiResp.Error.Code == 429 {
				return "", ErrRateLimited
			}
			return "", fmt.Errorf("gemini api error (%d): %s", geminiResp.Error.Code, geminiResp.Error.Message)
		}

		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			return "", errors.New("empty response from gemini")
		}

		rawText := geminiResp.Candidates[0].Content.Parts[0].Text
		return CleanAIResponse(rawText, c.cfg.BotMaxReplyChars), nil
	}

	return "", lastErr
}

// CleanAIResponse cleans markdown, removes extra whitespace, trims max length
func CleanAIResponse(raw string, maxChars int) string {
	text := strings.TrimSpace(raw)

	// Filter out harsh slang
	vulgarPattern := regexp.MustCompile(`(?i)\b(anjir|bjir|anjing|bego|goblok)\b`)
	text = vulgarPattern.ReplaceAllString(text, "waduh")

	// Filter out laugh expressions (wkwk, haha, hehe, xixi, etc.)
	laughPattern := regexp.MustCompile(`(?i)\b(w[kx]+[kx]*|ha(ha)+|he(he)+|xi(xi)+|kkk+|lol|😂|🤣)\b`)
	text = laughPattern.ReplaceAllString(text, "")
	// Clean up double spaces/commas left after removal
	text = regexp.MustCompile(`\s{2,}`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`^[,\s]+|[,\s]+$`).ReplaceAllString(text, "")
	text = strings.TrimSpace(text)

	// Trim length if needed
	if maxChars > 0 && len(text) > maxChars {
		// Cut at sensible boundary
		text = text[:maxChars]
		if lastDot := strings.LastIndex(text, "."); lastDot != -1 && lastDot > maxChars/2 {
			text = text[:lastDot+1]
		}
	}

	return text
}
