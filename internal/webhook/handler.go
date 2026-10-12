package webhook

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"temanwa-ai/internal/config"
	"temanwa-ai/internal/queue"
)

type MessageProcessor interface {
	ProcessMessage(ctx context.Context, msg *NormalizedMessage) error
}

type Handler struct {
	cfg       *config.Config
	processor MessageProcessor
	pool      *queue.WorkerPool
}

func NewHandler(cfg *config.Config, processor MessageProcessor, pool *queue.WorkerPool) *Handler {
	return &Handler{
		cfg:       cfg,
		processor: processor,
		pool:      pool,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read webhook body", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 1. Verify HMAC Signature if configured
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		sigHeader = r.Header.Get("X-Gowa-Signature")
	}
	if sigHeader == "" {
		sigHeader = r.Header.Get("X-Signature")
	}

	if err := VerifyHMAC(bodyBytes, sigHeader, h.cfg.GOWAWebhookSecret); err != nil {
		slog.Warn("Webhook HMAC signature verification failed", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Debug: log raw payload
	slog.Debug("Raw webhook payload", "body", string(bodyBytes))
	slog.Info("Webhook received", "body_length", len(bodyBytes), "body_preview", string(bodyBytes[:min(len(bodyBytes), 500)]))

	// 3. Normalize payload
	msg, err := Normalize(bodyBytes)
	if err != nil {
		// If payload is empty or not a message event, acknowledge cleanly
		if err == ErrUnsupportedEvent {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ignored_event"}`))
			return
		}
		slog.Warn("Failed to normalize webhook payload", "error", err)
		http.Error(w, "Unprocessable entity", http.StatusUnprocessableEntity)
		return
	}

	// 3. Dispatch to bounded worker pool
	submitted := h.pool.Submit(func(ctx context.Context) {
		if err := h.processor.ProcessMessage(ctx, msg); err != nil {
			slog.Error("Failed to process message", "id", msg.ID, "chat", msg.ChatJID, "error", err)
		}
	})

	if !submitted {
		slog.Warn("Worker pool queue is full, applying backpressure", "id", msg.ID)
		http.Error(w, "Server busy", http.StatusServiceUnavailable)
		return
	}

	// 4. Return fast 200 OK
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

// HealthHandler returns health status
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
