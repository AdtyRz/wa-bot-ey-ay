package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv   string
	LogLevel string
	Port     string

	// GOWA
	GOWABaseURL       string
	GOWADeviceID      string
	GOWAUsername      string
	GOWAPassword      string
	GOWAWebhookSecret string
	GOWAWebhookPath   string

	// Gemini
	GeminiAPIKey            string
	GeminiModel             string
	GeminiTemperature       float64
	GeminiMaxOutputTokens   int
	AIBudgetMode            string // "strict_free" or "manual"
	AIMaxRequestsPerUserHr  int
	AIMaxConcurrentRequests int

	// Supabase
	SupabaseDBURL         string
	SupabaseStorageEnable bool
	SupabaseMediaBucket   string

	// Bot Behavior
	BotPrefix                       string
	BotReplyInGroups                bool
	BotGroupReplyOnlyWhenMentioned  bool
	BotAllowedChats                 []string
	BotBlockedChats                 []string
	BotOwnerJIDs                    []string
	BotDefaultLanguage              string
	BotContextTurns                 int
	BotSummaryEnabled               bool
	BotMediaTempDir                 string
	BotMediaTempTTL                 time.Duration
	BotMaxImageMB                   int64
	BotMaxAudioMB                   int64
	BotMaxAudioMinutes              int
	BotMaxVideoMB                   int64
	BotMaxVideoSeconds              int
	BotMaxDocumentMB                int64
	BotMaxReplyChars                int

	// Trends / Grounding
	TrendSearchEnabled bool
	TrendCacheTTL      time.Duration
}

// Load loads configuration from environment and optional .env file
func Load() (*Config, error) {
	// Try loading .env if exists, ignore error if not found
	_ = godotenv.Load()

	cfg := &Config{
		AppEnv:                  getEnv("APP_ENV", "development"),
		LogLevel:                getEnv("APP_LOG_LEVEL", "info"),
		Port:                    getEnv("PORT", "8080"),
		GOWABaseURL:             strings.TrimRight(getEnv("GOWA_BASE_URL", "http://localhost:3000"), "/"),
		GOWADeviceID:            getEnv("GOWA_DEVICE_ID", ""),
		GOWAUsername:            getEnv("GOWA_USERNAME", ""),
		GOWAPassword:            getEnv("GOWA_PASSWORD", ""),
		GOWAWebhookSecret:       getEnv("GOWA_WEBHOOK_SECRET", ""),
		GOWAWebhookPath:         getEnv("GOWA_WEBHOOK_PATH", "/webhooks/gowa"),
		GeminiAPIKey:            getEnv("GEMINI_API_KEY", ""),
		GeminiModel:             getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		GeminiTemperature:       getEnvFloat("GEMINI_TEMPERATURE", 0.7),
		GeminiMaxOutputTokens:   getEnvInt("GEMINI_MAX_OUTPUT_TOKENS", 512),
		AIBudgetMode:            getEnv("AI_BUDGET_MODE", "strict_free"),
		AIMaxRequestsPerUserHr:  getEnvInt("AI_MAX_REQUESTS_PER_USER_PER_HOUR", 30),
		AIMaxConcurrentRequests: getEnvInt("AI_MAX_CONCURRENT_REQUESTS", 3),
		SupabaseDBURL:           getEnv("SUPABASE_DB_URL", ""),
		SupabaseStorageEnable:   getEnvBool("SUPABASE_STORAGE_ENABLED", false),
		SupabaseMediaBucket:     getEnv("SUPABASE_MEDIA_BUCKET", ""),
		BotPrefix:               getEnv("BOT_PREFIX", "."),
		BotReplyInGroups:        getEnvBool("BOT_REPLY_IN_GROUPS", false),
		BotGroupReplyOnlyWhenMentioned: getEnvBool("BOT_GROUP_REPLY_ONLY_WHEN_MENTIONED", true),
		BotAllowedChats:         splitComma(getEnv("BOT_ALLOWED_CHATS", "")),
		BotBlockedChats:         splitComma(getEnv("BOT_BLOCKED_CHATS", "")),
		BotOwnerJIDs:            splitComma(getEnv("BOT_OWNER_JIDS", "")),
		BotDefaultLanguage:      getEnv("BOT_DEFAULT_LANGUAGE", "id"),
		BotContextTurns:         getEnvInt("BOT_CONTEXT_TURNS", 16),
		BotSummaryEnabled:       getEnvBool("BOT_SUMMARY_ENABLED", true),
		BotMediaTempDir:         getEnv("BOT_MEDIA_TEMP_DIR", "/tmp/temanwa-media"),
		BotMediaTempTTL:         time.Duration(getEnvInt("BOT_MEDIA_TEMP_TTL_MINUTES", 15)) * time.Minute,
		BotMaxImageMB:           int64(getEnvInt("BOT_MAX_IMAGE_MB", 10)),
		BotMaxAudioMB:           int64(getEnvInt("BOT_MAX_AUDIO_MB", 15)),
		BotMaxAudioMinutes:      getEnvInt("BOT_MAX_AUDIO_MINUTES", 5),
		BotMaxVideoMB:           int64(getEnvInt("BOT_MAX_VIDEO_MB", 25)),
		BotMaxVideoSeconds:      getEnvInt("BOT_MAX_VIDEO_SECONDS", 90),
		BotMaxDocumentMB:        int64(getEnvInt("BOT_MAX_DOCUMENT_MB", 10)),
		BotMaxReplyChars:        getEnvInt("BOT_MAX_REPLY_CHARS", 1200),
		TrendSearchEnabled:      getEnvBool("TREND_SEARCH_ENABLED", false),
		TrendCacheTTL:           time.Duration(getEnvInt("TREND_CACHE_TTL_MINUTES", 30)) * time.Minute,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.AIBudgetMode != "strict_free" && c.AIBudgetMode != "manual" {
		return fmt.Errorf("invalid AI_BUDGET_MODE: must be 'strict_free' or 'manual'")
	}
	if c.AIMaxConcurrentRequests <= 0 {
		c.AIMaxConcurrentRequests = 3
	}
	if c.BotContextTurns <= 0 {
		c.BotContextTurns = 16
	}
	return nil
}

func (c *Config) IsOwner(jid string) bool {
	cleanJID := strings.TrimSpace(jid)
	for _, owner := range c.BotOwnerJIDs {
		if strings.TrimSpace(owner) == cleanJID {
			return true
		}
	}
	return false
}

func (c *Config) IsChatAllowed(jid string) bool {
	if len(c.BotAllowedChats) == 0 {
		return true // Allow all by default if list is empty
	}
	cleanJID := strings.TrimSpace(jid)
	for _, allowed := range c.BotAllowedChats {
		if strings.TrimSpace(allowed) == cleanJID {
			return true
		}
	}
	return false
}

func (c *Config) IsChatBlocked(jid string) bool {
	if len(c.BotBlockedChats) == 0 {
		return false
	}
	cleanJID := strings.TrimSpace(jid)
	for _, blocked := range c.BotBlockedChats {
		if strings.TrimSpace(blocked) == cleanJID {
			return true
		}
	}
	return false
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val, ok := os.LookupEnv(key); ok {
		if fVal, err := strconv.ParseFloat(val, 64); err == nil {
			return fVal
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		if bVal, err := strconv.ParseBool(val); err == nil {
			return bVal
		}
	}
	return defaultVal
}

func splitComma(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}
