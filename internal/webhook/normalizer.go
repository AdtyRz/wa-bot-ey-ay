package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	ErrEmptyPayload      = errors.New("empty webhook payload")
	ErrUnsupportedEvent  = errors.New("unsupported event type")
	ErrMissingMessageID  = errors.New("missing message id")
	ErrMissingChatID     = errors.New("missing chat id")
)

// Normalize parses incoming JSON bytes and returns a standardized NormalizedMessage
func Normalize(rawBytes []byte) (*NormalizedMessage, error) {
	if len(rawBytes) == 0 {
		return nil, ErrEmptyPayload
	}

	var root map[string]interface{}
	if err := json.Unmarshal(rawBytes, &root); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Check if this is a known non-message event
	event, _ := root["event"].(string)
	if event != "" && event != "message" && event != "messages.upsert" && event != "message.create" && event != "message.reaction" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEvent, event)
	}

	// Determine payload container
	data := root
	if p, ok := root["payload"].(map[string]interface{}); ok && p != nil {
		data = p
	}

	msg := &NormalizedMessage{
		Type:       MediaTypeText,
		RawPayload: root,
		Timestamp:  time.Now(),
	}

	// 1. Extract Message ID
	msg.ID = getString(data, "id", "message_id", "key_id", "stanza_id")
	if msg.ID == "" {
		if key, ok := data["key"].(map[string]interface{}); ok {
			msg.ID = getString(key, "id", "ID")
		}
	}
	if msg.ID == "" {
		return nil, ErrMissingMessageID
	}

	// 2. Extract Chat JID
	msg.ChatJID = getString(data, "chat", "chat_jid", "chat_id", "remote_jid", "remoteJid", "to", "recipient")
	if msg.ChatJID == "" {
		if key, ok := data["key"].(map[string]interface{}); ok {
			msg.ChatJID = getString(key, "remoteJid", "remote_jid")
		}
	}
	if msg.ChatJID == "" {
		return nil, ErrMissingChatID
	}
	msg.IsGroup = strings.HasSuffix(msg.ChatJID, "@g.us")
	if isGrp, ok := getBool(data, "is_group", "isGroup"); ok && isGrp {
		msg.IsGroup = true
	}

	// 3. Extract Sender JID
	msg.SenderJID = getString(data, "sender", "sender_jid", "participant", "from")
	if msg.SenderJID == "" {
		if key, ok := data["key"].(map[string]interface{}); ok {
			msg.SenderJID = getString(key, "participant")
		}
	}
	if msg.SenderJID == "" {
		if !msg.IsGroup {
			msg.SenderJID = msg.ChatJID
		}
	}

	// 4. Extract FromMe flag
	if fm, ok := getBool(data, "from_me", "fromMe", "is_from_me"); ok {
		msg.IsFromMe = fm
	} else if key, ok := data["key"].(map[string]interface{}); ok {
		if fm, ok := getBool(key, "fromMe", "from_me"); ok {
			msg.IsFromMe = fm
		}
	}

	// 5. Extract PushName
	msg.PushName = getString(data, "push_name", "pushName", "sender_name", "sender_display_name", "from_name")

	// 6. Extract Timestamp
	if ts := getInt64(data, "timestamp", "messageTimestamp"); ts > 0 {
		if ts > 1000000000000 { // Milliseconds
			msg.Timestamp = time.UnixMilli(ts)
		} else {
			msg.Timestamp = time.Unix(ts, 0)
		}
	} else if tsStr := getString(data, "timestamp"); tsStr != "" {
		// GoWA sends ISO 8601 timestamps as strings
		if parsed, err := time.Parse(time.RFC3339, tsStr); err == nil {
			msg.Timestamp = parsed
		}
	}

	// 7. Parse Message Content & Types
	rawMsg, _ := data["message"].(map[string]interface{})
	if rawMsg == nil {
		rawMsg = data
	}

	// Check simple conversation text
	if conv, ok := rawMsg["conversation"].(string); ok && conv != "" {
		msg.Type = MediaTypeText
		msg.Text = strings.TrimSpace(conv)
	}

	// Check extended text message (may contain quotes / contextInfo)
	if ext, ok := rawMsg["extendedTextMessage"].(map[string]interface{}); ok {
		msg.Type = MediaTypeText
		if text, ok := ext["text"].(string); ok {
			msg.Text = strings.TrimSpace(text)
		}
		parseContextInfo(ext, msg)
	}

	// Check Image message (standard WhatsMeow imageMessage or GoWA image object/string)
	img, hasImgMap := rawMsg["imageMessage"].(map[string]interface{})
	if !hasImgMap {
		img, hasImgMap = rawMsg["image"].(map[string]interface{})
	}
	if hasImgMap {
		msg.Type = MediaTypeImage
		msg.Caption = strings.TrimSpace(getString(img, "caption"))
		msg.MediaURL = getString(img, "url", "directPath", "file_url")
		relPath := getString(img, "file_path", "path")
		if relPath != "" {
			if !filepath.IsAbs(relPath) {
				msg.MediaPath = filepath.Join("/home/adtyaa/gowa_data", relPath)
			} else {
				msg.MediaPath = relPath
			}
		}
		msg.MediaMimeType = getString(img, "mimetype", "mime_type")
		msg.MediaSize = getInt64(img, "fileLength", "file_length", "size")
		parseContextInfo(img, msg)
	} else if imgStr, ok := rawMsg["image"].(string); ok && imgStr != "" {
		// GoWA can also send image as a direct relative filepath string
		msg.Type = MediaTypeImage
		if !filepath.IsAbs(imgStr) {
			msg.MediaPath = filepath.Join("/home/adtyaa/gowa_data", imgStr)
		} else {
			msg.MediaPath = imgStr
		}
		msg.MediaMimeType = "image/jpeg"
	}

	// Check Video message
	if vid, ok := rawMsg["videoMessage"].(map[string]interface{}); ok {
		msg.Type = MediaTypeVideo
		msg.Caption = strings.TrimSpace(getString(vid, "caption"))
		msg.MediaURL = getString(vid, "url", "directPath", "file_url")
		msg.MediaPath = getString(vid, "file_path", "path")
		msg.MediaMimeType = getString(vid, "mimetype", "mime_type")
		msg.MediaSize = getInt64(vid, "fileLength", "file_length", "size")
		msg.MediaDuration = int(getInt64(vid, "seconds", "duration"))
		parseContextInfo(vid, msg)
	}

	// Check Audio message / Voice note
	if aud, ok := rawMsg["audioMessage"].(map[string]interface{}); ok {
		msg.Type = MediaTypeAudio
		msg.MediaURL = getString(aud, "url", "directPath", "file_url")
		msg.MediaPath = getString(aud, "file_path", "path")
		msg.MediaMimeType = getString(aud, "mimetype", "mime_type")
		msg.MediaSize = getInt64(aud, "fileLength", "file_length", "size")
		msg.MediaDuration = int(getInt64(aud, "seconds", "duration"))
		if ptt, ok := aud["ptt"].(bool); ok {
			msg.IsVoiceNote = ptt
		}
		parseContextInfo(aud, msg)
	}

	// Check Sticker message (standard stickerMessage or GoWA sticker field)
	stkMap, hasStkMap := rawMsg["stickerMessage"].(map[string]interface{})
	if !hasStkMap {
		stkMap, hasStkMap = rawMsg["sticker"].(map[string]interface{})
	}
	if hasStkMap {
		msg.Type = MediaTypeSticker
		msg.MediaURL = getString(stkMap, "url", "directPath", "file_url")
		relPath := getString(stkMap, "file_path", "path")
		if relPath != "" {
			if !filepath.IsAbs(relPath) {
				msg.MediaPath = filepath.Join("/home/adtyaa/gowa_data", relPath)
			} else {
				msg.MediaPath = relPath
			}
		}
		msg.MediaMimeType = getString(stkMap, "mimetype", "mime_type")
		msg.MediaSize = getInt64(stkMap, "fileLength", "file_length", "size")
		if anim, ok := stkMap["isAnimated"].(bool); ok {
			msg.IsAnimated = anim
		}
		parseContextInfo(stkMap, msg)
	} else if stkStr, ok := rawMsg["sticker"].(string); ok && stkStr != "" {
		// GoWA sends sticker as direct filepath string, e.g. "statics/media/xxx.webp"
		msg.Type = MediaTypeSticker
		if !filepath.IsAbs(stkStr) {
			msg.MediaPath = filepath.Join("/home/adtyaa/gowa_data", stkStr)
		} else {
			msg.MediaPath = stkStr
		}
		msg.MediaMimeType = "image/webp"
	}

	// Check Document message
	if doc, ok := rawMsg["documentMessage"].(map[string]interface{}); ok {
		msg.Type = MediaTypeDocument
		msg.Caption = strings.TrimSpace(getString(doc, "title", "fileName", "caption"))
		msg.MediaURL = getString(doc, "url", "directPath", "file_url")
		msg.MediaPath = getString(doc, "file_path", "path")
		msg.MediaMimeType = getString(doc, "mimetype", "mime_type")
		msg.MediaSize = getInt64(doc, "fileLength", "file_length", "size")
		parseContextInfo(doc, msg)
	}

	// Flat field fallbacks if nested parser didn't catch text or media
	if msg.Text == "" && msg.Type == MediaTypeText {
		if reaction := getString(data, "reaction"); reaction != "" {
			msg.Text = fmt.Sprintf("[Mereaksi pesan dengan emotikon: %s]", reaction)
		} else {
			msg.Text = strings.TrimSpace(getString(data, "text", "body", "message_text", "conversation"))
		}
	}
	if msg.Type == MediaTypeText && msg.Text == "" {
		// Check flat media types
		mType := strings.ToLower(getString(data, "media_type", "type"))
		switch mType {
		case "image":
			msg.Type = MediaTypeImage
		case "video":
			msg.Type = MediaTypeVideo
		case "audio":
			msg.Type = MediaTypeAudio
		case "sticker":
			msg.Type = MediaTypeSticker
		case "document":
			msg.Type = MediaTypeDocument
		}
		if msg.MediaURL == "" {
			msg.MediaURL = getString(data, "media_url", "url")
		}
		if msg.MediaPath == "" {
			msg.MediaPath = getString(data, "media_path", "path")
		}
		if msg.Caption == "" {
			msg.Caption = getString(data, "caption")
		}
	}

	return msg, nil
}

func parseContextInfo(container map[string]interface{}, msg *NormalizedMessage) {
	ci, ok := container["contextInfo"].(map[string]interface{})
	if !ok || ci == nil {
		return
	}

	if stanzaID, ok := ci["stanzaId"].(string); ok {
		msg.QuotedID = stanzaID
	}
	if participant, ok := ci["participant"].(string); ok {
		msg.QuotedSender = participant
	}

	if qm, ok := ci["quotedMessage"].(map[string]interface{}); ok {
		if conv, ok := qm["conversation"].(string); ok {
			msg.QuotedText = conv
		} else if ext, ok := qm["extendedTextMessage"].(map[string]interface{}); ok {
			if txt, ok := ext["text"].(string); ok {
				msg.QuotedText = txt
			}
		} else if img, ok := qm["imageMessage"].(map[string]interface{}); ok {
			msg.QuotedText = "[Gambar] " + getString(img, "caption")
		} else if vid, ok := qm["videoMessage"].(map[string]interface{}); ok {
			msg.QuotedText = "[Video] " + getString(vid, "caption")
		} else if aud, ok := qm["audioMessage"].(map[string]interface{}); ok {
			_ = aud
			msg.QuotedText = "[Audio/Voice Note]"
		} else if stk, ok := qm["stickerMessage"].(map[string]interface{}); ok {
			_ = stk
			msg.QuotedText = "[Stiker]"
		} else if doc, ok := qm["documentMessage"].(map[string]interface{}); ok {
			msg.QuotedText = "[Dokumen] " + getString(doc, "title", "fileName")
		}
	}
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func getBool(m map[string]interface{}, keys ...string) (bool, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if b, ok := v.(bool); ok {
				return b, true
			}
		}
	}
	return false, false
}

func getInt64(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch val := v.(type) {
			case float64:
				return int64(val)
			case int64:
				return val
			case int:
				return int64(val)
			case string:
				if n, err := strconv.ParseInt(val, 10, 64); err == nil {
					return n
				}
			}
		}
	}
	return 0
}
