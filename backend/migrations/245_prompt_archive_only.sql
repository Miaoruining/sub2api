-- Store only the client-supplied conversation context. This table is
-- intentionally independent from Prompt Audit jobs/events and has no identity,
-- credential, usage, policy, scanner, or model-response columns.
CREATE TABLE IF NOT EXISTS request_context_archives (
    id               BIGSERIAL PRIMARY KEY,
    request_id       VARCHAR(128) NOT NULL DEFAULT '',
    protocol         VARCHAR(64) NOT NULL DEFAULT '',
    model            VARCHAR(255) NOT NULL DEFAULT '',
    stage            VARCHAR(32) NOT NULL DEFAULT 'http',
    context_payload  JSONB NOT NULL,
    content_hash     VARCHAR(64) NOT NULL,
    message_count    INT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_request_context_archives_payload
        CHECK (jsonb_typeof(context_payload) = 'array'),
    CONSTRAINT chk_request_context_archives_message_count
        CHECK (message_count > 0),
    CONSTRAINT uq_request_context_archives_idempotency
        UNIQUE (request_id, stage, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_request_context_archives_created
    ON request_context_archives(created_at DESC, id DESC);
