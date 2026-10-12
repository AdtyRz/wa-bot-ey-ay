package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrSendFailed = errors.New("failed to send message via GOWA")
)

type Client interface {
	SendText(ctx context.Context, chatJID, text string, quotedMsgID string) error
	SendMedia(ctx context.Context, chatJID, mediaType, mediaURL, caption, quotedMsgID string) error
	SendPresence(ctx context.Context, chatJID string, presenceType string) error
	Ping(ctx context.Context) error
}

type GOWAClient struct {
	baseURL    string
	username   string
	password   string
	deviceID   string
	httpClient *http.Client
}

func NewGOWAClient(baseURL, username, password, deviceID string, timeout time.Duration) *GOWAClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &GOWAClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		deviceID:   deviceID,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type SendMessagePayload struct {
	Phone          string `json:"phone"`
	Message        string `json:"message"`
	ReplyMessageID string `json:"reply_message_id,omitempty"`
}

type SendMediaPayload struct {
	Phone          string `json:"phone"`
	Caption        string `json:"caption,omitempty"`
	URL            string `json:"url,omitempty"`
	ReplyMessageID string `json:"reply_message_id,omitempty"`
}

// FormatPhone strips @s.whatsapp.net or @g.us for GOWA endpoints that expect clean phone/jid
func FormatPhone(jid string) string {
	clean := strings.TrimSpace(jid)
	if strings.HasSuffix(clean, "@s.whatsapp.net") {
		return strings.TrimSuffix(clean, "@s.whatsapp.net")
	}
	// For groups, keep the @g.us or as-is
	return clean
}

func (c *GOWAClient) SendText(ctx context.Context, chatJID, text string, quotedMsgID string) error {
	phone := FormatPhone(chatJID)
	payload := SendMessagePayload{
		Phone:          phone,
		Message:        text,
		ReplyMessageID: quotedMsgID,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// GOWA standard endpoint: /send/message
	url := fmt.Sprintf("%s/send/message", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	if c.deviceID != "" {
		req.Header.Set("X-Device-Id", c.deviceID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d, body: %s", ErrSendFailed, resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *GOWAClient) SendMedia(ctx context.Context, chatJID, mediaType, mediaURL, caption, quotedMsgID string) error {
	phone := FormatPhone(chatJID)
	endpoint := "send/image"
	switch mediaType {
	case "video":
		endpoint = "send/video"
	case "audio":
		endpoint = "send/audio"
	case "document":
		endpoint = "send/file"
	}

	payload := SendMediaPayload{
		Phone:          phone,
		Caption:        caption,
		URL:            mediaURL,
		ReplyMessageID: quotedMsgID,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d, body: %s", ErrSendFailed, resp.StatusCode, string(respBody))
	}

	return nil
}

type SendPresencePayload struct {
	Phone  string `json:"phone"`
	Action string `json:"action"`
}

func (c *GOWAClient) SendPresence(ctx context.Context, chatJID string, presenceType string) error {
	action := "start"
	if presenceType == "paused" || presenceType == "stop" {
		action = "stop"
	}

	payload := SendPresencePayload{
		Phone:  chatJID, // Send full JID (supports groups @g.us & private @s.whatsapp.net)
		Action: action,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/send/chat-presence", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	if c.deviceID != "" {
		req.Header.Set("X-Device-Id", c.deviceID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *GOWAClient) Ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/app/devices", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed with status %d", resp.StatusCode)
	}
	return nil
}
