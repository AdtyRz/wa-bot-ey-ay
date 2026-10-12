package storage

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type MemoryRepo struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation      // chatJID -> Conversation
	messages      map[string][]*Message         // conversationID -> []Message
	userMemories  map[string][]*UserMemory      // userJID -> []UserMemory
	events        map[string]*ProcessedEvent    // eventKey -> ProcessedEvent
	trendCache    map[string]*TrendCacheItem    // queryHash -> TrendCacheItem
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		conversations: make(map[string]*Conversation),
		messages:      make(map[string][]*Message),
		userMemories:  make(map[string][]*UserMemory),
		events:        make(map[string]*ProcessedEvent),
		trendCache:    make(map[string]*TrendCacheItem),
	}
}

func (m *MemoryRepo) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryRepo) Close() error {
	return nil
}

func (m *MemoryRepo) GetOrCreateConversation(ctx context.Context, chatJID string) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, exists := m.conversations[chatJID]
	if !exists {
		conv = &Conversation{
			ID:            fmt.Sprintf("conv-%d", time.Now().UnixNano()),
			ChatJID:       chatJID,
			Mode:          ModeActive,
			LastMessageAt: time.Now(),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		m.conversations[chatJID] = conv
	}
	return conv, nil
}

func (m *MemoryRepo) UpdateConversationMode(ctx context.Context, chatJID string, mode ConversationMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.conversations[chatJID]; ok {
		conv.Mode = mode
		conv.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MemoryRepo) UpdateConversationSummary(ctx context.Context, chatJID, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.conversations[chatJID]; ok {
		conv.Summary = summary
		conv.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MemoryRepo) ClearConversation(ctx context.Context, chatJID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.conversations[chatJID]; ok {
		delete(m.messages, conv.ID)
		conv.Summary = ""
		conv.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MemoryRepo) SaveMessage(ctx context.Context, msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	// Avoid duplicates for same conversation and whatsapp message id
	list := m.messages[msg.ConversationID]
	for _, existing := range list {
		if existing.WhatsAppMessageID == msg.WhatsAppMessageID && msg.WhatsAppMessageID != "" {
			return nil
		}
	}

	m.messages[msg.ConversationID] = append(list, msg)
	return nil
}

func (m *MemoryRepo) GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.messages[conversationID]
	if len(list) == 0 {
		return []*Message{}, nil
	}

	if limit <= 0 || limit > len(list) {
		limit = len(list)
	}

	// Return last 'limit' items in chronological order
	start := len(list) - limit
	result := make([]*Message, limit)
	copy(result, list[start:])
	return result, nil
}

func (m *MemoryRepo) SaveUserMemory(ctx context.Context, memory *UserMemory) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if memory.ID == "" {
		memory.ID = fmt.Sprintf("mem-%d", time.Now().UnixNano())
	}
	m.userMemories[memory.UserJID] = append(m.userMemories[memory.UserJID], memory)
	return nil
}

func (m *MemoryRepo) GetUserMemories(ctx context.Context, userJID string) ([]*UserMemory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	memList := m.userMemories[userJID]
	result := make([]*UserMemory, 0, len(memList))
	now := time.Now()
	for _, mem := range memList {
		if mem.ConsentStatus == "granted" && (mem.ExpiresAt == nil || mem.ExpiresAt.After(now)) {
			result = append(result, mem)
		}
	}
	return result, nil
}

func (m *MemoryRepo) DeleteUserMemories(ctx context.Context, userJID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.userMemories, userJID)
	return nil
}

func (m *MemoryRepo) TryLockEvent(ctx context.Context, eventKey, eventType string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ev, exists := m.events[eventKey]
	if !exists {
		m.events[eventKey] = &ProcessedEvent{
			EventKey:     eventKey,
			EventType:    eventType,
			Status:       "processing",
			AttemptCount: 1,
			CreatedAt:    time.Now(),
		}
		return true, nil
	}

	if ev.Status == "failed" {
		ev.Status = "processing"
		ev.AttemptCount++
		return true, nil
	}

	// Already processing or completed
	return false, nil
}

func (m *MemoryRepo) CompleteEvent(ctx context.Context, eventKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ev, ok := m.events[eventKey]; ok {
		ev.Status = "completed"
		now := time.Now()
		ev.ProcessedAt = &now
	}
	return nil
}

func (m *MemoryRepo) FailEvent(ctx context.Context, eventKey, errorCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ev, ok := m.events[eventKey]; ok {
		ev.Status = "failed"
		ev.LastErrorCode = errorCode
	}
	return nil
}

func (m *MemoryRepo) GetTrendCache(ctx context.Context, queryHash string) (*TrendCacheItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, ok := m.trendCache[queryHash]
	if !ok || item.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}
	return item, nil
}

func (m *MemoryRepo) SaveTrendCache(ctx context.Context, item *TrendCacheItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.trendCache[item.QueryHash] = item
	return nil
}
