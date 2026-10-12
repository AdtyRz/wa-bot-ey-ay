package storage

import (
	"context"
	"time"
)

type ConversationMode string

const (
	ModeActive ConversationMode = "active"
	ModePaused ConversationMode = "paused"
)

type Conversation struct {
	ID            string           `json:"id"`
	ChatJID       string           `json:"chat_jid"`
	Mode          ConversationMode `json:"mode"`
	Summary       string           `json:"summary,omitempty"`
	LastMessageAt time.Time        `json:"last_message_at"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type Message struct {
	ID                string                 `json:"id"`
	ConversationID    string                 `json:"conversation_id"`
	WhatsAppMessageID string                 `json:"whatsapp_message_id"`
	SenderJID         string                 `json:"sender_jid"`
	Direction         string                 `json:"direction"` // "inbound" or "outbound"
	MessageType       string                 `json:"message_type"`
	TextContent       string                 `json:"text_content"`
	QuotedMessageID   string                 `json:"quoted_message_id,omitempty"`
	MediaMetadata     map[string]interface{} `json:"media_metadata,omitempty"`
	ProcessingStatus  string                 `json:"processing_status"`
	CreatedAt         time.Time              `json:"created_at"`
}

type UserMemory struct {
	ID              string     `json:"id"`
	UserJID         string     `json:"user_jid"`
	MemoryKey       string     `json:"memory_key"`
	MemoryValue     string     `json:"memory_value"`
	SourceMessageID string     `json:"source_message_id,omitempty"`
	ConsentStatus   string     `json:"consent_status"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ProcessedEvent struct {
	EventKey      string     `json:"event_key"`
	EventType     string     `json:"event_type"`
	Status        string     `json:"status"` // "processing", "completed", "failed"
	AttemptCount  int        `json:"attempt_count"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type TrendCacheItem struct {
	QueryHash   string                 `json:"query_hash"`
	QueryText   string                 `json:"query_text"`
	ResultJSON  map[string]interface{} `json:"result_json"`
	SourcesJSON []interface{}          `json:"sources_json"`
	FetchedAt   time.Time              `json:"fetched_at"`
	ExpiresAt   time.Time              `json:"expires_at"`
}

type Repository interface {
	// Conversations & Context
	GetOrCreateConversation(ctx context.Context, chatJID string) (*Conversation, error)
	UpdateConversationMode(ctx context.Context, chatJID string, mode ConversationMode) error
	UpdateConversationSummary(ctx context.Context, chatJID, summary string) error
	ClearConversation(ctx context.Context, chatJID string) error

	// Messages
	SaveMessage(ctx context.Context, msg *Message) error
	GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error)

	// User Memory
	SaveUserMemory(ctx context.Context, memory *UserMemory) error
	GetUserMemories(ctx context.Context, userJID string) ([]*UserMemory, error)
	DeleteUserMemories(ctx context.Context, userJID string) error

	// Event Deduplication / Idempotency
	// TryLockEvent returns true if this event is new and can be processed, false if already processing/completed
	TryLockEvent(ctx context.Context, eventKey, eventType string) (bool, error)
	CompleteEvent(ctx context.Context, eventKey string) error
	FailEvent(ctx context.Context, eventKey, errorCode string) error

	// Trend Cache
	GetTrendCache(ctx context.Context, queryHash string) (*TrendCacheItem, error)
	SaveTrendCache(ctx context.Context, item *TrendCacheItem) error

	// Health
	Ping(ctx context.Context) error
	Close() error
}
