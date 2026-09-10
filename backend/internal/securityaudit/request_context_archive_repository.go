package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
)

// ArchiveRepository is separate from Prompt Audit's JobRepository and writes
// only the request-context archive table. It intentionally has no query/list
// API and returns no audit event.
type ArchiveRepository interface {
	RecordArchive(ctx context.Context, archive RequestContextArchive) error
}

// RecordArchive inserts one request-context archive. The unique key makes
// retries from the asynchronous hot-path idempotent without creating audit
// jobs, Redis payloads, or scanner results.
func (r *PostgreSQLRepository) RecordArchive(ctx context.Context, archive RequestContextArchive) error {
	if r == nil || r.db == nil {
		return errors.New("request context archive database unavailable")
	}
	if len(archive.ContextPayload) == 0 || archive.MessageCount != len(archive.ContextPayload) || archive.ContentHash == "" {
		return errors.New("request context archive payload invalid")
	}
	payload, err := json.Marshal(archive.ContextPayload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO request_context_archives (
			request_id, protocol, model, stage, context_payload, content_hash, message_count
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7)
		ON CONFLICT (request_id, stage, content_hash) DO NOTHING`,
		archive.RequestID, archive.Protocol, archive.Model, normalizeStage(archive.Stage), string(payload), archive.ContentHash, archive.MessageCount)
	return err
}

// Compile-time documentation that the concrete repository provides the
// optional archive capability without changing the Prompt Audit interfaces.
var _ ArchiveRepository = (*PostgreSQLRepository)(nil)
