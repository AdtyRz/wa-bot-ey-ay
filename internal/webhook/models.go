package webhook

import (
	"time"
)

type MediaType string

const (
	MediaTypeText     MediaType = "text"
	MediaTypeImage    MediaType = "image"
	MediaTypeVideo    MediaType = "video"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeSticker  MediaType = "sticker"
	MediaTypeDocument MediaType = "document"
	MediaTypeOther    MediaType = "other"
)

// NormalizedMessage represents a standardized message across different webhook payload versions
type NormalizedMessage struct {
	ID             string                 `json:"id"`
	ChatJID        string                 `json:"chat_jid"`
	SenderJID      string                 `json:"sender_jid"`
	PushName       string                 `json:"push_name"`
	IsFromMe       bool                   `json:"is_from_me"`
	IsGroup        bool                   `json:"is_group"`
	Timestamp      time.Time              `json:"timestamp"`
	Type           MediaType              `json:"type"`
	Text           string                 `json:"text"`
	Caption        string                 `json:"caption"`
	QuotedID       string                 `json:"quoted_id,omitempty"`
	QuotedText     string                 `json:"quoted_text,omitempty"`
	QuotedSender   string                 `json:"quoted_sender,omitempty"`
	MediaURL       string                 `json:"media_url,omitempty"`
	MediaPath      string                 `json:"media_path,omitempty"`
	MediaMimeType  string                 `json:"media_mimetype,omitempty"`
	MediaSize      int64                  `json:"media_size,omitempty"`
	MediaDuration  int                    `json:"media_duration,omitempty"` // in seconds
	IsAnimated     bool                   `json:"is_animated,omitempty"`
	IsVoiceNote    bool                   `json:"is_voice_note,omitempty"`
	RawPayload     map[string]interface{} `json:"raw_payload,omitempty"`
}

// GOWAWebhookPayload is a generic container for incoming GOWA events
type GOWAWebhookPayload struct {
	Event     string                 `json:"event"`
	DeviceID  string                 `json:"device_id,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
	// Direct fields if sent without "payload" wrapper
	ID        string                 `json:"id,omitempty"`
	MessageID string                 `json:"message_id,omitempty"`
	Chat      string                 `json:"chat,omitempty"`
	ChatJID   string                 `json:"chat_jid,omitempty"`
	Sender    string                 `json:"sender,omitempty"`
	SenderJID string                 `json:"sender_jid,omitempty"`
	FromMe    *bool                  `json:"from_me,omitempty"`
	IsFromMe  *bool                  `json:"is_from_me,omitempty"`
	Timestamp int64                  `json:"timestamp,omitempty"`
	Message   map[string]interface{} `json:"message,omitempty"`
}
