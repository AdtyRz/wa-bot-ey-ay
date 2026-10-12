package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"temanwa-ai/internal/ai/gemini"
	"temanwa-ai/internal/ai/prompts"
	"temanwa-ai/internal/ai/trends"
	"temanwa-ai/internal/commands"
	"temanwa-ai/internal/config"
	"temanwa-ai/internal/conversation"
	"temanwa-ai/internal/media"
	"temanwa-ai/internal/storage"
	"temanwa-ai/internal/webhook"
)

// TerminalWAClient simulates WhatsApp sending by printing directly to terminal
type TerminalWAClient struct{}

func (t *TerminalWAClient) SendText(ctx context.Context, chatJID, text string, quotedMsgID string) error {
	fmt.Printf("\n🤖 TemanWA: %s\n\n", text)
	return nil
}

func (t *TerminalWAClient) SendMedia(ctx context.Context, chatJID, mediaType, mediaURL, caption, quotedMsgID string) error {
	fmt.Printf("\n🤖 TemanWA [Media %s]: %s\n\n", mediaType, caption)
	return nil
}

func (t *TerminalWAClient) Ping(ctx context.Context) error {
	return nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Gagal memuat config: %v\n", err)
		return
	}

	if cfg.GeminiAPIKey == "" {
		fmt.Println("⚠️  PERINGATAN: GEMINI_API_KEY belum diisi di file .env !")
		fmt.Println("Silakan buat file .env dan isi GEMINI_API_KEY=kunci_api_anda")
		fmt.Println("Atau jalankan: export GEMINI_API_KEY=\"kunci_api_anda\"\n")
	}

	fmt.Println("==================================================")
	fmt.Println("   TemanWA AI - Interactive Terminal Testing Mode ")
	fmt.Println("==================================================")
	fmt.Printf("Model Gemini: %s (Hemat Token)\n", cfg.GeminiModel)
	fmt.Printf("Budget Mode : %s\n", cfg.AIBudgetMode)
	fmt.Println("Ketik pesan Anda (atau ketik .menu, .reset, .ping, keluar: 'exit'/'quit')")
	fmt.Println("--------------------------------------------------")

	repo := storage.NewMemoryRepo()
	waClient := &TerminalWAClient{}
	mediaMgr, _ := media.NewManager(cfg)
	promptMgr, _ := prompts.NewManager("prompts/persona_id.md")
	cmdHandler := commands.NewHandler(cfg, repo)
	trendService := trends.NewTrendService(cfg, repo)
	aiClient := gemini.NewGeminiClient(cfg)

	orchestrator := conversation.NewOrchestrator(
		cfg,
		repo,
		waClient,
		aiClient,
		mediaMgr,
		promptMgr,
		cmdHandler,
		trendService,
	)

	scanner := bufio.NewScanner(os.Stdin)
	chatJID := "test_user@s.whatsapp.net"

	for {
		fmt.Print("👤 Kamu: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("Dadah! Sesi testing selesai.")
			break
		}

		// Check if user simulates sending video
		msgType := webhook.MediaTypeText
		if input == "[video]" || strings.HasPrefix(input, "video:") {
			msgType = webhook.MediaTypeVideo
		}

		msg := &webhook.NormalizedMessage{
			ID:        fmt.Sprintf("test-%d", time.Now().UnixNano()),
			ChatJID:   chatJID,
			SenderJID: chatJID,
			Type:      msgType,
			Text:      input,
			Timestamp: time.Now(),
		}

		err := orchestrator.ProcessMessage(context.Background(), msg)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	}
}
