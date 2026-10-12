package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"temanwa-ai/internal/storage"
)

type PostgresRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresRepo(ctx context.Context, dbURL string) (*PostgresRepo, error) {
	if dbURL == "" {
		return nil, errors.New("empty database url")
	}

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}

	config.MaxConns = 15
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresRepo{pool: pool}, nil
}

func (r *PostgresRepo) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepo) Close() error {
	r.pool.Close()
	return nil
}

func (r *PostgresRepo) GetOrCreateConversation(ctx context.Context, chatJID string) (*storage.Conversation, error) {
	query := `
		INSERT INTO conversations (chat_jid, mode, last_message_at, created_at, updated_at)
		VALUES ($1, 'active', NOW(), NOW(), NOW())
		ON CONFLICT (chat_jid) DO UPDATE SET updated_at = NOW()
		RETURNING id, chat_jid, mode, COALESCE(summary, ''), last_message_at, created_at, updated_at
	`
	conv := &storage.Conversation{}
	var summary string
	err := r.pool.QueryRow(ctx, query, chatJID).Scan(
		&conv.ID,
		&conv.ChatJID,
		&conv.Mode,
		&summary,
		&conv.LastMessageAt,
		&conv.CreatedAt,
		&conv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	conv.Summary = summary
	return conv, nil
}

func (r *PostgresRepo) UpdateConversationMode(ctx context.Context, chatJID string, mode storage.ConversationMode) error {
	query := `UPDATE conversations SET mode = $1, updated_at = NOW() WHERE chat_jid = $2`
	_, err := r.pool.Exec(ctx, query, string(mode), chatJID)
	return err
}

func (r *PostgresRepo) UpdateConversationSummary(ctx context.Context, chatJID, summary string) error {
	query := `UPDATE conversations SET summary = $1, updated_at = NOW() WHERE chat_jid = $2`
	_, err := r.pool.Exec(ctx, query, summary, chatJID)
	return err
}

func (r *PostgresRepo) ClearConversation(ctx context.Context, chatJID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var convID string
	err = tx.QueryRow(ctx, `SELECT id FROM conversations WHERE chat_jid = $1`, chatJID).Scan(&convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE conversation_id = $1`, convID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET summary = NULL, updated_at = NOW() WHERE id = $1`, convID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepo) SaveMessage(ctx context.Context, msg *storage.Message) error {
	metaJSON, err := json.Marshal(msg.MediaMetadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	query := `
		INSERT INTO messages (
			conversation_id, whatsapp_message_id, sender_jid, direction,
			message_type, text_content, quoted_message_id, media_metadata,
			processing_status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (conversation_id, whatsapp_message_id) DO NOTHING
		RETURNING id
	`
	var id string
	err = r.pool.QueryRow(ctx, query,
		msg.ConversationID,
		msg.WhatsAppMessageID,
		msg.SenderJID,
		msg.Direction,
		msg.MessageType,
		msg.TextContent,
		msg.QuotedMessageID,
		metaJSON,
		msg.ProcessingStatus,
	).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	msg.ID = id

	// Update conversation last_message_at
	_, _ = r.pool.Exec(ctx, `UPDATE conversations SET last_message_at = NOW() WHERE id = $1`, msg.ConversationID)
	return nil
}

func (r *PostgresRepo) GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]*storage.Message, error) {
	if limit <= 0 {
		limit = 16
	}

	query := `
		SELECT id, conversation_id, whatsapp_message_id, sender_jid, direction,
		       message_type, COALESCE(text_content, ''), COALESCE(quoted_message_id, ''),
		       media_metadata, processing_status, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var descMessages []*storage.Message
	for rows.Next() {
		m := &storage.Message{}
		var metaBytes []byte
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.WhatsAppMessageID, &m.SenderJID, &m.Direction,
			&m.MessageType, &m.TextContent, &m.QuotedMessageID, &metaBytes,
			&m.ProcessingStatus, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &m.MediaMetadata)
		}
		descMessages = append(descMessages, m)
	}

	// Reverse to chronological order (oldest first)
	n := len(descMessages)
	messages := make([]*storage.Message, n)
	for i, m := range descMessages {
		messages[n-1-i] = m
	}

	return messages, nil
}

func (r *PostgresRepo) SaveUserMemory(ctx context.Context, memory *storage.UserMemory) error {
	query := `
		INSERT INTO user_memory (user_jid, memory_key, memory_value, source_message_id, consent_status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id
	`
	return r.pool.QueryRow(ctx, query,
		memory.UserJID,
		memory.MemoryKey,
		memory.MemoryValue,
		memory.SourceMessageID,
		memory.ConsentStatus,
		memory.ExpiresAt,
	).Scan(&memory.ID)
}

func (r *PostgresRepo) GetUserMemories(ctx context.Context, userJID string) ([]*storage.UserMemory, error) {
	query := `
		SELECT id, user_jid, memory_key, memory_value, COALESCE(source_message_id, ''), consent_status, expires_at, created_at, updated_at
		FROM user_memory
		WHERE user_jid = $1 AND consent_status = 'granted' AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, query, userJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []*storage.UserMemory
	for rows.Next() {
		mem := &storage.UserMemory{}
		if err := rows.Scan(
			&mem.ID, &mem.UserJID, &mem.MemoryKey, &mem.MemoryValue,
			&mem.SourceMessageID, &mem.ConsentStatus, &mem.ExpiresAt,
			&mem.CreatedAt, &mem.UpdatedAt,
		); err != nil {
			return nil, err
		}
		memories = append(memories, mem)
	}
	return memories, nil
}

func (r *PostgresRepo) DeleteUserMemories(ctx context.Context, userJID string) error {
	query := `DELETE FROM user_memory WHERE user_jid = $1`
	_, err := r.pool.Exec(ctx, query, userJID)
	return err
}

func (r *PostgresRepo) TryLockEvent(ctx context.Context, eventKey, eventType string) (bool, error) {
	query := `
		INSERT INTO processed_events (event_key, event_type, status, attempt_count, created_at)
		VALUES ($1, $2, 'processing', 1, NOW())
		ON CONFLICT (event_key) DO UPDATE
		SET attempt_count = processed_events.attempt_count + 1
		WHERE processed_events.status = 'failed'
		RETURNING status
	`
	var status string
	err := r.pool.QueryRow(ctx, query, eventKey, eventType).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already processing or completed
			return false, nil
		}
		return false, err
	}
	return status == "processing", nil
}

func (r *PostgresRepo) CompleteEvent(ctx context.Context, eventKey string) error {
	query := `UPDATE processed_events SET status = 'completed', processed_at = NOW() WHERE event_key = $1`
	_, err := r.pool.Exec(ctx, query, eventKey)
	return err
}

func (r *PostgresRepo) FailEvent(ctx context.Context, eventKey, errorCode string) error {
	query := `UPDATE processed_events SET status = 'failed', last_error_code = $1 WHERE event_key = $2`
	_, err := r.pool.Exec(ctx, query, errorCode, eventKey)
	return err
}

func (r *PostgresRepo) GetTrendCache(ctx context.Context, queryHash string) (*storage.TrendCacheItem, error) {
	query := `
		SELECT query_hash, query_text, result_json, sources_json, fetched_at, expires_at
		FROM trend_cache
		WHERE query_hash = $1 AND expires_at > NOW()
	`
	item := &storage.TrendCacheItem{}
	var resultBytes, sourcesBytes []byte
	err := r.pool.QueryRow(ctx, query, queryHash).Scan(
		&item.QueryHash, &item.QueryText, &resultBytes, &sourcesBytes, &item.FetchedAt, &item.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(resultBytes) > 0 {
		_ = json.Unmarshal(resultBytes, &item.ResultJSON)
	}
	if len(sourcesBytes) > 0 {
		_ = json.Unmarshal(sourcesBytes, &item.SourcesJSON)
	}
	return item, nil
}

func (r *PostgresRepo) SaveTrendCache(ctx context.Context, item *storage.TrendCacheItem) error {
	resBytes, err := json.Marshal(item.ResultJSON)
	if err != nil {
		resBytes = []byte("{}")
	}
	srcBytes, err := json.Marshal(item.SourcesJSON)
	if err != nil {
		srcBytes = []byte("[]")
	}

	query := `
		INSERT INTO trend_cache (query_hash, query_text, result_json, sources_json, fetched_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (query_hash) DO UPDATE
		SET result_json = EXCLUDED.result_json,
		    sources_json = EXCLUDED.sources_json,
		    fetched_at = EXCLUDED.fetched_at,
		    expires_at = EXCLUDED.expires_at
	`
	_, err = r.pool.Exec(ctx, query, item.QueryHash, item.QueryText, resBytes, srcBytes, item.FetchedAt, item.ExpiresAt)
	return err
}
