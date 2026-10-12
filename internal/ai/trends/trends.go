package trends

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/storage"
)

type TrendResult struct {
	HasData   bool
	Summary   string
	Sources   []string
	Timestamp time.Time
}

type TrendService struct {
	cfg  *config.Config
	repo storage.Repository
}

func NewTrendService(cfg *config.Config, repo storage.Repository) *TrendService {
	return &TrendService{cfg: cfg, repo: repo}
}

// IsTrendQuery checks if the user's message is asking for current viral trends / news
func IsTrendQuery(text string) bool {
	lower := strings.ToLower(text)
	keywords := []string{
		"lagi viral", "yang viral", "tren hari ini", "trend hari ini",
		"lagi rame", "yang lagi rame", "berita viral", "meme viral",
		"apa yang lagi viral", "lagi hits",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hashQuery(q string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(q))))
	return hex.EncodeToString(h[:])
}

// GetTrendInfo retrieves trend information or returns honest fallback if grounding is unavailable
func (s *TrendService) GetTrendInfo(ctx context.Context, query string) (*TrendResult, string) {
	if !s.cfg.TrendSearchEnabled {
		// Grounding not enabled per PRD Section 5.3 & 12
		fallback := "Aku belum bisa memastikan tren yang viral hari ini tanpa cek sumber terbaru. Kalau kamu ada link atau meme yang mau dibahas, kirim aja biar kubantu jelasin!"
		return &TrendResult{HasData: false}, fallback
	}

	qHash := hashQuery(query)
	// Check cache
	cached, err := s.repo.GetTrendCache(ctx, qHash)
	if err == nil && cached != nil {
		sources := make([]string, 0, len(cached.SourcesJSON))
		for _, s := range cached.SourcesJSON {
			sources = append(sources, fmt.Sprint(s))
		}
		summary, _ := cached.ResultJSON["summary"].(string)
		return &TrendResult{
			HasData:   true,
			Summary:   summary,
			Sources:   sources,
			Timestamp: cached.FetchedAt,
		}, ""
	}

	// In MVP without external Google Search Grounding active, fallback gracefully
	fallback := "Aku belum bisa memastikan tren yang viral hari ini tanpa cek sumber terbaru. Kalau kamu ada link atau meme yang mau dibahas, kirim aja biar kubantu jelasin!"
	return &TrendResult{HasData: false}, fallback
}
