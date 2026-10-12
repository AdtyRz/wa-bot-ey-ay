package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/storage"
	"temanwa-ai/internal/webhook"
)

type Handler struct {
	cfg       *config.Config
	repo      storage.Repository
	startTime time.Time
}

func NewHandler(cfg *config.Config, repo storage.Repository) *Handler {
	return &Handler{
		cfg:       cfg,
		repo:      repo,
		startTime: time.Now(),
	}
}

// IsCommand checks whether the message text starts with the bot prefix
func (h *Handler) IsCommand(text string) bool {
	prefix := h.cfg.BotPrefix
	if prefix == "" {
		prefix = "."
	}
	return strings.HasPrefix(strings.TrimSpace(text), prefix)
}

// Handle executes a recognized command and returns the reply text (or empty if not handled)
func (h *Handler) Handle(ctx context.Context, msg *webhook.NormalizedMessage) (string, error) {
	prefix := h.cfg.BotPrefix
	if prefix == "" {
		prefix = "."
	}

	trimmed := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(trimmed, prefix) {
		return "", nil
	}

	cmdStr := strings.TrimPrefix(trimmed, prefix)
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", nil
	}

	cmd := strings.ToLower(parts[0])
	_ = parts[1:]

	switch cmd {
	case "menu", "help":
		return h.handleHelp(), nil
	case "ping":
		return "pong, aman.", nil
	case "status":
		return h.handleStatus(), nil
	case "mute", "pause", "ignore", "off":
		return h.handleMute(ctx, msg.ChatJID)
	case "unmute", "resume", "unignore", "on":
		return h.handleUnmute(ctx, msg.ChatJID)
	default:
		return fmt.Sprintf("ga ada command itu, cek %smenu", prefix), nil
	}
}

func (h *Handler) handleHelp() string {
	p := h.cfg.BotPrefix
	return fmt.Sprintf(`list command:

- %smenu / %shelp : Bantuan perintah.
- %sping : Cek koneksi bot.
- %sstatus : Cek status bot.
- %smute : Matikan respons bot di chat/grup ini.
- %sunmute : Aktifkan kembali respons bot.`, p, p, p, p, p, p)
}

func (h *Handler) handleStatus() string {
	uptime := time.Since(h.startTime).Round(time.Second)
	return fmt.Sprintf("status: aman 🟢\nuptime: %s", uptime)
}

func (h *Handler) handleMute(ctx context.Context, chatJID string) (string, error) {
	if err := h.repo.UpdateConversationMode(ctx, chatJID, storage.ModePaused); err != nil {
		return "gagal mute bot", err
	}
	return "sip, bot di-mute dlu.", nil
}

func (h *Handler) handleUnmute(ctx context.Context, chatJID string) (string, error) {
	if err := h.repo.UpdateConversationMode(ctx, chatJID, storage.ModeActive); err != nil {
		return "gagal aktifin bot", err
	}
	return "sip, bot aktif lagi.", nil
}
