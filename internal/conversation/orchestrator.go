package conversation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"temanwa-ai/internal/ai/gemini"
	"temanwa-ai/internal/ai/prompts"
	"temanwa-ai/internal/ai/trends"
	"temanwa-ai/internal/commands"
	"temanwa-ai/internal/config"
	"temanwa-ai/internal/media"
	"temanwa-ai/internal/storage"
	"temanwa-ai/internal/webhook"
	"temanwa-ai/internal/whatsapp"
)

var (
	videoExcuses = []string{
		"adugh memorinya abis ga bisa buka video 😭",
		"video apa ini ga bisa dibuka, hp gue ngelag",
		"aduh gabisa buka video, ceritain intinya aja",
		"video apa ini ga bisa dibuka, muter-muter terus",
	}
)

type Orchestrator struct {
	cfg          *config.Config
	repo         storage.Repository
	waClient     whatsapp.Client
	aiClient     gemini.AIClient
	mediaManager *media.Manager
	promptMgr    *prompts.Manager
	cmdHandler   *commands.Handler
	trendService *trends.TrendService
	rng          *rand.Rand

	relayMu          sync.RWMutex
	relayTargets     map[string]string // targetJID -> ownerJID
	contactNameCache map[string]string // targetJID -> name
}

func NewOrchestrator(
	cfg *config.Config,
	repo storage.Repository,
	waClient whatsapp.Client,
	aiClient gemini.AIClient,
	mediaMgr *media.Manager,
	promptMgr *prompts.Manager,
	cmdHandler *commands.Handler,
	trendService *trends.TrendService,
) *Orchestrator {
	return &Orchestrator{
		cfg:              cfg,
		repo:             repo,
		waClient:         waClient,
		aiClient:         aiClient,
		mediaManager:     mediaMgr,
		promptMgr:        promptMgr,
		cmdHandler:       cmdHandler,
		trendService:     trendService,
		rng:              rand.New(rand.NewSource(time.Now().UnixNano())),
		relayTargets:     make(map[string]string),
		contactNameCache: make(map[string]string),
	}
}

// ProcessMessage orchestrates incoming message processing
func (o *Orchestrator) ProcessMessage(ctx context.Context, msg *webhook.NormalizedMessage) error {
	slog.Info("ProcessMessage started", "chat", msg.ChatJID, "sender", msg.SenderJID, "text", msg.Text, "from_me", msg.IsFromMe)

	// 1. Guard against bot's own messages
	if msg.IsFromMe {
		slog.Debug("Ignoring message from bot itself", "id", msg.ID)
		return nil
	}

	// 2. Chat allowlist filter & blocked chats
	if !o.cfg.IsChatAllowed(msg.ChatJID) || o.cfg.IsChatBlocked(msg.ChatJID) {
		slog.Debug("Chat not allowed or blocked", "chat", msg.ChatJID)
		return nil
	}

	// 3. Group chat rules
	if msg.IsGroup {
		if !o.cfg.BotReplyInGroups {
			slog.Debug("Group replies disabled", "chat", msg.ChatJID)
			return nil
		}
	}

	// 4. Idempotency lock
	eventKey := fmt.Sprintf("%s:%s", msg.ChatJID, msg.ID)
	locked, err := o.repo.TryLockEvent(ctx, eventKey, string(msg.Type))
	if err != nil {
		slog.Error("Failed to check idempotency lock", "event_key", eventKey, "error", err)
		return err
	}
	if !locked {
		slog.Info("Duplicate or already processing event ignored", "event_key", eventKey)
		return nil
	}

	// Make sure we update event status on completion/failure
	var processingErr error
	defer func() {
		if processingErr != nil {
			_ = o.repo.FailEvent(ctx, eventKey, processingErr.Error())
		} else {
			_ = o.repo.CompleteEvent(ctx, eventKey)
		}
	}()

	// 5. Get conversation session
	conv, err := o.repo.GetOrCreateConversation(ctx, msg.ChatJID)
	if err != nil {
		processingErr = err
		return fmt.Errorf("failed to get conversation: %w", err)
	}

	// Check if this sender is a monitored relay target replying back
	if ownerJID, targetName, exists := o.getRelayOwner(msg.SenderJID); exists {
		displayName := targetName
		if displayName == "" {
			displayName = msg.SenderJID
		}
		forwardText := fmt.Sprintf("📩 Pesan dari %s:\n\"%s\"", displayName, msg.Text)
		_ = o.waClient.SendText(ctx, ownerJID, forwardText, "")

		// Send short chill acknowledgment to target
		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "oke udh diterusin.", msg.ID)
		return nil
	}

	// Check if this is an instruction from Owner (kirim pesan, mute group, simpan kontak)
	if o.cfg.IsOwner(msg.SenderJID) && msg.Type == webhook.MediaTypeText {
		handled, err := o.handleOwnerInstruction(ctx, conv, msg)
		if err != nil {
			slog.Error("Failed to handle owner instruction", "error", err)
		}
		if handled {
			return nil
		}
	}

	// Check if user requests data deletion in natural language
	lowerText := strings.ToLower(strings.TrimSpace(msg.Text))
	if msg.Type == webhook.MediaTypeText && (lowerText == "hapus data" || lowerText == "hapus data gw" || lowerText == "hapus data saya" || lowerText == "hapus memori" || lowerText == "hapus memoriku") {
		_ = o.repo.DeleteUserMemories(ctx, msg.SenderJID)
		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "data lu udh dihapus.", msg.ID)
		return nil
	}

	// 6. Check for bot commands
	if msg.Type == webhook.MediaTypeText && o.cmdHandler.IsCommand(msg.Text) {
		replyText, cmdErr := o.cmdHandler.Handle(ctx, msg)
		if cmdErr != nil {
			slog.Warn("Command execution returned error", "cmd", msg.Text, "error", cmdErr)
		}
		if replyText != "" {
			_ = o.waClient.SendText(ctx, msg.ChatJID, replyText, msg.ID)
		}
		return nil
	}

	// 7. Check if conversation is paused / muted
	if conv.Mode == storage.ModePaused {
		slog.Debug("Conversation is paused, skipping AI reply", "chat", msg.ChatJID)
		return nil
	}

	// 8. Enforce AI Budget / User Quota (Owner bypasses quota)
	if !o.cfg.IsOwner(msg.SenderJID) && !o.aiClient.CheckQuota(msg.SenderJID) {
		slog.Warn("User exceeded hourly quota", "sender", msg.SenderJID)
		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "Limit chat kamu udh abis buat sejam ini. Nanti lagi ya.", msg.ID)
		return nil
	}

	// 9. Save inbound message to history
	inboundMsg := &storage.Message{
		ConversationID:    conv.ID,
		WhatsAppMessageID: msg.ID,
		SenderJID:         msg.SenderJID,
		Direction:         "inbound",
		MessageType:       string(msg.Type),
		TextContent:       msg.Text,
		QuotedMessageID:   msg.QuotedID,
		ProcessingStatus:  "processing",
	}
	if msg.Caption != "" {
		if inboundMsg.TextContent == "" {
			inboundMsg.TextContent = msg.Caption
		} else {
			inboundMsg.TextContent += " " + msg.Caption
		}
	}
	_ = o.repo.SaveMessage(ctx, inboundMsg)

	// Send typing indicator ("composing") to chat
	_ = o.waClient.SendPresence(ctx, msg.ChatJID, "composing")

	// 10. Handle by message type
	var replyText string
	isOwner := o.cfg.IsOwner(msg.SenderJID)

	switch msg.Type {
	case webhook.MediaTypeVideo:
		// User requirement: For video, return random funny excuse!
		// e.g. "adugh memorinya abis ga bisa buka video" or "video apa ini ga bisa di buka"
		replyText = o.getRandomVideoExcuse()

	case webhook.MediaTypeImage, webhook.MediaTypeSticker, webhook.MediaTypeAudio, webhook.MediaTypeDocument:
		replyText, err = o.handleMultimodal(ctx, conv, msg)
		if err != nil {
			slog.Warn("Multimodal processing error", "type", msg.Type, "error", err)
			replyText = o.getMultimodalFallback(isOwner, msg.Type, err)
		}

	case webhook.MediaTypeText:
		replyText, err = o.handleText(ctx, conv, msg)
		if err != nil {
			slog.Warn("Text processing error", "error", err)
			replyText = o.getTextFallback(isOwner, err)
		}

	default:
		replyText = "Waduh, format pesannya belum kubisa baca nih. Kirim teks atau foto aja ya!"
	}

	// 11. Send reply and record outbound message
	if strings.TrimSpace(replyText) != "" {
		slog.Info("Sending AI reply", "chat", msg.ChatJID, "reply", replyText)
		err = o.sendReply(ctx, conv.ID, msg.ChatJID, replyText, msg.ID)
		if err != nil {
			slog.Error("Failed to send reply to WhatsApp", "chat", msg.ChatJID, "error", err)
			processingErr = err
			return fmt.Errorf("failed to send reply: %w", err)
		}
		slog.Info("AI reply successfully sent", "chat", msg.ChatJID)
		_ = o.waClient.SendPresence(ctx, msg.ChatJID, "paused")
	}

	return nil
}

func (o *Orchestrator) getRandomVideoExcuse() string {
	idx := o.rng.Intn(len(videoExcuses))
	return videoExcuses[idx]
}

func (o *Orchestrator) handleText(ctx context.Context, conv *storage.Conversation, msg *webhook.NormalizedMessage) (string, error) {
	// Check viral trend queries
	var trendContext string
	if trends.IsTrendQuery(msg.Text) {
		res, fallback := o.trendService.GetTrendInfo(ctx, msg.Text)
		if !res.HasData {
			return fallback, nil
		}
		trendContext = fmt.Sprintf("Ringkasan: %s\nSumber: %s (Tanggal: %s)",
			res.Summary, strings.Join(res.Sources, ", "), res.Timestamp.Format("02-01-2006"))
	}

	// Fetch recent history
	history, _ := o.repo.GetRecentMessages(ctx, conv.ID, o.cfg.BotContextTurns)
	userMemories, _ := o.repo.GetUserMemories(ctx, msg.SenderJID)

	// Build context prompt
	// Build context prompt and truncate to avoid Gemini input limit
	contextPrompt := o.promptMgr.BuildConversationContext(history, conv.Summary, userMemories, msg.QuotedText, trendContext)
	fullPrompt := fmt.Sprintf("%s\n\nPesan Pengguna Terbaru: %s", contextPrompt, msg.Text)
	// Truncate if exceeds approx 8000 characters
	if len(fullPrompt) > 8000 {
		// Trim oldest part of contextPrompt
		excess := len(fullPrompt) - 8000
		if len(contextPrompt) > excess {
			contextPrompt = contextPrompt[excess:]
		} else {
			contextPrompt = ""
		}
		fullPrompt = fmt.Sprintf("%s\n\nPesan Pengguna Terbaru: %s", contextPrompt, msg.Text)
	}

	// Choose system prompt
	var systemPrompt string
	if o.cfg.IsOwner(msg.SenderJID) {
		systemPrompt = "Kamu adalah asisten AI pribadi untuk Owner bot. Nada bicara profesional, sopan, jelas, dan solutif. Jika pertanyaan singkat atau santai, balas to the point dan ringkas. Jika owner bertanya panjang atau membutuhkan penjelasan, jawab dengan jelas, lugas, dan terstruktur tanpa bertele-tele. DILARANG menggunakan kata tawa (wkwk, haha, dll)."
	} else {
		systemPrompt = o.promptMgr.GetSystemPrompt()
	}

	// Call Gemini
	return o.aiClient.GenerateText(ctx, systemPrompt, fullPrompt)
}

func (o *Orchestrator) handleMultimodal(ctx context.Context, conv *storage.Conversation, msg *webhook.NormalizedMessage) (string, error) {
	processed, err := o.mediaManager.PrepareMedia(ctx, msg)
	if err != nil {
		return "", err
	}
	defer o.mediaManager.Cleanup(processed.LocalPath)

	mediaBytes, err := os.ReadFile(processed.LocalPath)
	if err != nil {
		return "", fmt.Errorf("failed to read media file: %w", err)
	}

	history, _ := o.repo.GetRecentMessages(ctx, conv.ID, o.cfg.BotContextTurns)
	contextPrompt := o.promptMgr.BuildConversationContext(history, conv.Summary, nil, msg.QuotedText, "")

	userPrompt := msg.Caption
	if userPrompt == "" {
		switch msg.Type {
		case webhook.MediaTypeImage:
			userPrompt = "Tolong perhatikan gambar ini dan berikan tanggapan singkat santai yang sesuai konteks."
		case webhook.MediaTypeSticker:
			userPrompt = "Tanggapi stiker ini secara sosial dan santai."
		case webhook.MediaTypeAudio:
			if msg.IsVoiceNote {
				userPrompt = "Dengarkan voice note ini dan tanggapi intinya secara singkat."
			} else {
				userPrompt = "Dengarkan audio ini dan tanggapi isinya."
			}
		case webhook.MediaTypeDocument:
			userPrompt = "Ringkas dokumen ini secara singkat dan padat."
		}
	}

	fullPrompt := fmt.Sprintf("%s\n\nPertanyaan/Permintaan Pengguna: %s", contextPrompt, userPrompt)

	var systemPrompt string
	if o.cfg.IsOwner(msg.SenderJID) {
		systemPrompt = "Kamu adalah asisten AI pribadi untuk Owner bot. Tanggapi media ini secara profesional, akurat, to the point, dan informatif. DILARANG menggunakan kata tawa (wkwk, haha, dll)."
	} else {
		systemPrompt = o.promptMgr.GetSystemPrompt()
	}

	return o.aiClient.GenerateMultimodal(ctx, systemPrompt, fullPrompt, mediaBytes, processed.MimeType)
}

func (o *Orchestrator) sendReply(ctx context.Context, convID, chatJID, text, quotedID string) error {
	// Send to WhatsApp via GOWA
	err := o.waClient.SendText(ctx, chatJID, text, quotedID)
	if err != nil {
		return err
	}

	// Record outbound message in storage
	outbound := &storage.Message{
		ConversationID:    convID,
		WhatsAppMessageID: fmt.Sprintf("out-%d", time.Now().UnixNano()),
		SenderJID:         chatJID,
		Direction:         "outbound",
		MessageType:       "text",
		TextContent:       text,
		QuotedMessageID:   quotedID,
		ProcessingStatus:  "processed",
	}
	_ = o.repo.SaveMessage(ctx, outbound)

	return nil
}

func (o *Orchestrator) getMultimodalFallback(isOwner bool, mType webhook.MediaType, err error) string {
	if errors.Is(err, media.ErrFileTooLarge) {
		return "filenya kegedean, coba kirim yg lebih kecil"
	}
	if errors.Is(err, media.ErrDurationTooLong) {
		return "durasinya kepanjangan, potong dlu"
	}
	if errors.Is(err, media.ErrUnsupportedMime) {
		return "format filenya ga kebaca"
	}
	return "medianya gagal dibuka, coba kirim ulang"
}

func (o *Orchestrator) getTextFallback(isOwner bool, err error) string {
	if errors.Is(err, gemini.ErrQuotaExceeded) {
		return "kuota mikir gw lg abis, ntar lg ya"
	}
	if errors.Is(err, gemini.ErrRateLimited) {
		return "antrean lg rame, sabar bentar ya"
	}
	return "otak gw lg nge-blank barusan, coba ulang"
}

// Helper methods for Relay, Forwarding, and Contact Management

func (o *Orchestrator) setRelayTarget(targetJID, ownerJID, contactName string) {
	o.relayMu.Lock()
	defer o.relayMu.Unlock()
	o.relayTargets[targetJID] = ownerJID
	if contactName != "" {
		o.contactNameCache[targetJID] = contactName
	}
}

func (o *Orchestrator) getRelayOwner(targetJID string) (string, string, bool) {
	o.relayMu.RLock()
	defer o.relayMu.RUnlock()
	ownerJID, exists := o.relayTargets[targetJID]
	name := o.contactNameCache[targetJID]
	return ownerJID, name, exists
}

func normalizePhone(input string) string {
	clean := strings.TrimSpace(input)
	clean = strings.TrimPrefix(clean, "+")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.TrimSuffix(clean, "@s.whatsapp.net")
	if strings.HasPrefix(clean, "08") {
		clean = "628" + clean[2:]
	} else if strings.HasPrefix(clean, "8") {
		clean = "628" + clean[1:]
	}
	return clean
}

func (o *Orchestrator) saveContact(ctx context.Context, ownerJID, name, phone string) (string, error) {
	cleanPhone := normalizePhone(phone)
	if cleanPhone == "" {
		return "", errors.New("nomor hp tidak valid")
	}
	jid := cleanPhone + "@s.whatsapp.net"
	cleanName := strings.ToLower(strings.TrimSpace(name))

	mem := &storage.UserMemory{
		UserJID:       ownerJID,
		MemoryKey:     "contact:" + cleanName,
		MemoryValue:   jid,
		ConsentStatus: "granted",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	err := o.repo.SaveUserMemory(ctx, mem)
	if err != nil {
		return "", err
	}
	o.relayMu.Lock()
	o.contactNameCache[jid] = name
	o.relayMu.Unlock()
	return jid, nil
}

func (o *Orchestrator) findContact(ctx context.Context, ownerJID, nameOrPhone string) (string, string) {
	target := strings.TrimSpace(nameOrPhone)
	digitsOnly := strings.TrimPrefix(target, "+")
	digitsOnly = strings.ReplaceAll(digitsOnly, "-", "")
	digitsOnly = strings.ReplaceAll(digitsOnly, " ", "")

	isNumber := true
	for _, c := range digitsOnly {
		if c < '0' || c > '9' {
			isNumber = false
			break
		}
	}
	if isNumber && len(digitsOnly) >= 9 {
		cleanPhone := normalizePhone(digitsOnly)
		jid := cleanPhone + "@s.whatsapp.net"
		return jid, digitsOnly
	}

	memories, err := o.repo.GetUserMemories(ctx, ownerJID)
	if err == nil {
		targetLower := strings.ToLower(target)
		for _, m := range memories {
			if strings.EqualFold(m.MemoryKey, "contact:"+targetLower) {
				return m.MemoryValue, target
			}
		}
		for _, m := range memories {
			if strings.HasPrefix(m.MemoryKey, "contact:") {
				savedName := strings.TrimPrefix(m.MemoryKey, "contact:")
				if strings.Contains(savedName, targetLower) || strings.Contains(targetLower, savedName) {
					return m.MemoryValue, savedName
				}
			}
		}
	}

	return "", target
}

func (o *Orchestrator) handleOwnerInstruction(ctx context.Context, conv *storage.Conversation, msg *webhook.NormalizedMessage) (bool, error) {
	text := strings.TrimSpace(msg.Text)
	textLower := strings.ToLower(text)

	// 1. Mute / ignore group instruction
	if msg.IsGroup && (strings.Contains(textLower, "jangan balas di grup ini") ||
		strings.Contains(textLower, "jangan respon di grup ini") ||
		strings.Contains(textLower, "mute grup ini") ||
		strings.Contains(textLower, "abaikan grup ini")) {
		_ = o.repo.UpdateConversationMode(ctx, msg.ChatJID, storage.ModePaused)
		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "sip, ga bakal respon di sini lagi.", msg.ID)
		return true, nil
	}

	// 2. Save contact instruction: "simpan kontak [nama] [nomor]" atau "simpan nomor [nama] [nomor]"
	savePattern := regexp.MustCompile(`(?i)^(?:simpan kontak|simpan nomor|kontak baru)\s+([a-zA-Z0-9_\-]+)\s+([0-9+\- ]+)`)
	if matches := savePattern.FindStringSubmatch(text); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		phone := strings.TrimSpace(matches[2])
		_, err := o.saveContact(ctx, msg.SenderJID, name, phone)
		if err != nil {
			_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "gagal nyimpen kontak.", msg.ID)
			return true, err
		}
		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "sip, nomor "+name+" udh disimpan.", msg.ID)
		return true, nil
	}

	// 3. Send message instruction: "kirim pesan kepada [nama] agar [pesan]", "chat [nama]: [pesan]"
	sendPrefixes := []string{
		"kirim pesan kepada ",
		"kirim pesan ke ",
		"kirim wa ke ",
		"chat kepada ",
		"chat ke ",
		"chat si ",
		"pesan ke ",
	}

	var matchedPrefix string
	for _, p := range sendPrefixes {
		if strings.HasPrefix(textLower, p) {
			matchedPrefix = p
			break
		}
	}

	if matchedPrefix != "" {
		contentAfterPrefix := strings.TrimSpace(text[len(matchedPrefix):])
		var targetIdentifier, messageBody string

		if idx := strings.Index(contentAfterPrefix, ":"); idx != -1 {
			targetIdentifier = strings.TrimSpace(contentAfterPrefix[:idx])
			messageBody = strings.TrimSpace(contentAfterPrefix[idx+1:])
		} else {
			connWords := []string{" agar ", " biar ", " kalau ", " untuk ", " suruh ", " supaya "}
			foundConn := false
			for _, cw := range connWords {
				if idx := strings.Index(strings.ToLower(contentAfterPrefix), cw); idx != -1 {
					targetIdentifier = strings.TrimSpace(contentAfterPrefix[:idx])
					messageBody = strings.TrimSpace(contentAfterPrefix[idx+len(cw):])
					foundConn = true
					break
				}
			}
			if !foundConn {
				parts := strings.Fields(contentAfterPrefix)
				if len(parts) >= 2 {
					targetIdentifier = parts[0]
					messageBody = strings.Join(parts[1:], " ")
				}
			}
		}

		if targetIdentifier == "" || messageBody == "" {
			_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "formatnya: kirim pesan ke [nama/nomor]: [pesan]", msg.ID)
			return true, nil
		}

		targetJID, targetName := o.findContact(ctx, msg.SenderJID, targetIdentifier)
		if targetJID == "" {
			_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "nomor "+targetName+" blm ada. simpen dlu: simpan kontak "+targetName+" 08xxx", msg.ID)
			return true, nil
		}

		// Send message to target
		err := o.waClient.SendText(ctx, targetJID, messageBody, "")
		if err != nil {
			_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "gagal kirim pesan ke "+targetName, msg.ID)
			return true, err
		}

		// Register relay target so when target replies, it forwards to owner
		o.setRelayTarget(targetJID, msg.SenderJID, targetName)

		_ = o.sendReply(ctx, conv.ID, msg.ChatJID, "udh dikirim ke "+targetName+". ntar kalo dia bales gw terusin ke lu.", msg.ID)
		return true, nil
	}

	return false, nil
}
