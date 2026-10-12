-- 001_initial_schema.sql: Supabase / PostgreSQL schema for TemanWA AI

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Bot Settings Table
CREATE TABLE IF NOT EXISTS bot_settings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key TEXT NOT NULL UNIQUE,
    value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Conversations Table
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    chat_jid TEXT NOT NULL UNIQUE,
    mode TEXT NOT NULL DEFAULT 'active', -- active, paused
    summary TEXT,
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_chat_jid ON conversations (chat_jid);

-- 3. Messages Table
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    whatsapp_message_id TEXT NOT NULL,
    sender_jid TEXT NOT NULL,
    direction TEXT NOT NULL, -- inbound, outbound
    message_type TEXT NOT NULL, -- text, image, video, audio, sticker, document, other
    text_content TEXT,
    quoted_message_id TEXT,
    media_metadata JSONB DEFAULT '{}'::jsonb,
    processing_status TEXT NOT NULL DEFAULT 'processed', -- pending, processed, failed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_conv_wamid ON messages (conversation_id, whatsapp_message_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages (conversation_id, created_at DESC);

-- 4. User Memory Table (Personal Long-term facts, opt-in)
CREATE TABLE IF NOT EXISTS user_memory (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_jid TEXT NOT NULL,
    memory_key TEXT NOT NULL,
    memory_value TEXT NOT NULL,
    source_message_id TEXT,
    consent_status TEXT NOT NULL DEFAULT 'granted', -- granted, revoked
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_memory_user_jid ON user_memory (user_jid);

-- 5. Processed Events (Idempotency and deduplication)
CREATE TABLE IF NOT EXISTS processed_events (
    event_key TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    status TEXT NOT NULL, -- processing, completed, failed
    attempt_count INTEGER NOT NULL DEFAULT 1,
    processed_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_processed_events_created_at ON processed_events (created_at DESC);

-- 6. Trend Cache Table (Grounding cache)
CREATE TABLE IF NOT EXISTS trend_cache (
    query_hash TEXT PRIMARY KEY,
    query_text TEXT NOT NULL,
    result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    sources_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trend_cache_expires_at ON trend_cache (expires_at);
