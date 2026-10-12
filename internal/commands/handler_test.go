package commands

import (
	"context"
	"strings"
	"testing"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/storage"
	"temanwa-ai/internal/webhook"
)

func TestCommandHandler(t *testing.T) {
	cfg := &config.Config{BotPrefix: "."}
	repo := storage.NewMemoryRepo()
	handler := NewHandler(cfg, repo)
	ctx := context.Background()

	// 1. .ping
	msgPing := &webhook.NormalizedMessage{Text: ".ping", ChatJID: "user@s.whatsapp.net"}
	reply, err := handler.Handle(ctx, msgPing)
	if err != nil || !strings.Contains(strings.ToLower(reply), "pong") {
		t.Errorf("Expected pong reply, got %s, err: %v", reply, err)
	}

	// 2. .menu / .help
	msgHelp := &webhook.NormalizedMessage{Text: ".help", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgHelp)
	if err != nil || !strings.Contains(reply, ".menu") {
		t.Errorf("Expected menu help text, got %s, err: %v", reply, err)
	}

	// 3. .status
	msgStatus := &webhook.NormalizedMessage{Text: ".status", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgStatus)
	if err != nil || !strings.Contains(strings.ToLower(reply), "status") {
		t.Errorf("Expected status reply, got %s, err: %v", reply, err)
	}

	// 4. .mute and .unmute
	msgMute := &webhook.NormalizedMessage{Text: ".mute", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgMute)
	if err != nil || !strings.Contains(strings.ToLower(reply), "mute") {
		t.Errorf("Expected mute reply, got %s, err: %v", reply, err)
	}

	msgUnmute := &webhook.NormalizedMessage{Text: ".unmute", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgUnmute)
	if err != nil || !strings.Contains(strings.ToLower(reply), "aktif") {
		t.Errorf("Expected unmute reply, got %s, err: %v", reply, err)
	}

	// 5. .clear
	msgClear := &webhook.NormalizedMessage{Text: ".clear", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgClear)
	if err != nil || !strings.Contains(strings.ToLower(reply), "dibersihin") {
		t.Errorf("Expected clear confirmation, got %s, err: %v", reply, err)
	}

	// 6. .hapusdata
	msgHapus := &webhook.NormalizedMessage{Text: ".hapusdata", ChatJID: "user@s.whatsapp.net", SenderJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgHapus)
	if err != nil || !strings.Contains(strings.ToLower(reply), "dihapus") {
		t.Errorf("Expected hapusdata confirmation, got %s, err: %v", reply, err)
	}

	// 7. Unknown command
	msgUnknown := &webhook.NormalizedMessage{Text: ".randomcmd", ChatJID: "user@s.whatsapp.net"}
	reply, err = handler.Handle(ctx, msgUnknown)
	if err != nil || !strings.Contains(reply, ".menu") {
		t.Errorf("Expected unknown command reply, got %s, err: %v", reply, err)
	}
}
