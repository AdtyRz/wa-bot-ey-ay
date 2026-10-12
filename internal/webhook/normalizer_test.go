package webhook

import (
	"testing"
)

func TestNormalize_TextMessage(t *testing.T) {
	raw := []byte(`{
		"event": "message",
		"payload": {
			"id": "MSG_1001",
			"chat": "628111222333@s.whatsapp.net",
			"sender": "628111222333@s.whatsapp.net",
			"push_name": "Budi",
			"from_me": false,
			"timestamp": 1728000000,
			"message": {
				"conversation": "Halo bot santai!"
			}
		}
	}`)

	msg, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}

	if msg.ID != "MSG_1001" {
		t.Errorf("Expected ID MSG_1001, got %s", msg.ID)
	}
	if msg.ChatJID != "628111222333@s.whatsapp.net" {
		t.Errorf("Expected ChatJID, got %s", msg.ChatJID)
	}
	if msg.Type != MediaTypeText {
		t.Errorf("Expected MediaTypeText, got %s", msg.Type)
	}
	if msg.Text != "Halo bot santai!" {
		t.Errorf("Expected Text, got %s", msg.Text)
	}
	if msg.IsFromMe {
		t.Error("Expected IsFromMe=false")
	}
}

func TestNormalize_ImageMessageWithCaption(t *testing.T) {
	raw := []byte(`{
		"event": "message",
		"payload": {
			"id": "MSG_IMG_1",
			"chat": "628111222333@s.whatsapp.net",
			"sender": "628111222333@s.whatsapp.net",
			"from_me": false,
			"message": {
				"imageMessage": {
					"caption": "ini apaan ya?",
					"url": "https://example.com/sample.jpg",
					"mimetype": "image/jpeg",
					"fileLength": 102400
				}
			}
		}
	}`)

	msg, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}

	if msg.Type != MediaTypeImage {
		t.Errorf("Expected MediaTypeImage, got %s", msg.Type)
	}
	if msg.Caption != "ini apaan ya?" {
		t.Errorf("Expected Caption, got %s", msg.Caption)
	}
	if msg.MediaMimeType != "image/jpeg" {
		t.Errorf("Expected mimetype image/jpeg, got %s", msg.MediaMimeType)
	}
}

func TestNormalize_QuotedMessage(t *testing.T) {
	raw := []byte(`{
		"event": "message",
		"payload": {
			"id": "MSG_REPLY_1",
			"chat": "628111222333@s.whatsapp.net",
			"sender": "628111222333@s.whatsapp.net",
			"from_me": false,
			"message": {
				"extendedTextMessage": {
					"text": "terus gimana?",
					"contextInfo": {
						"stanzaId": "ORIGINAL_123",
						"participant": "628999@s.whatsapp.net",
						"quotedMessage": {
							"conversation": "laptop gue kemarin tiba-tiba bluescreen"
						}
					}
				}
			}
		}
	}`)

	msg, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize failed: %v", err)
	}

	if msg.QuotedID != "ORIGINAL_123" {
		t.Errorf("Expected QuotedID ORIGINAL_123, got %s", msg.QuotedID)
	}
	if msg.QuotedText != "laptop gue kemarin tiba-tiba bluescreen" {
		t.Errorf("Expected QuotedText, got %s", msg.QuotedText)
	}
}

func TestNormalize_UnsupportedEvent(t *testing.T) {
	raw := []byte(`{"event":"call_received","payload":{}}`)
	_, err := Normalize(raw)
	if err == nil {
		t.Fatal("Expected error for unsupported event")
	}
}
