package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Enqueuer struct {
	config  ConfigStore
	repo    JobRepository
	payload PayloadStore
	metrics Metrics
}

func NewEnqueuer(config ConfigStore, repo JobRepository, payload PayloadStore, metrics ...Metrics) *Enqueuer {
	var metric Metrics
	if len(metrics) > 0 {
		metric = metrics[0]
	}
	return &Enqueuer{config: config, repo: repo, payload: payload, metrics: metric}
}

func (e *Enqueuer) Enqueue(ctx context.Context, req Request) error {
	if e == nil || e.config == nil || e.repo == nil || e.payload == nil {
		return errors.New("prompt audit enqueuer unavailable")
	}
	cfg, ok := e.config.Active()
	baseFields := requestLogFields(req)
	if !ok || cfg.EffectiveMode() != ModeAsync {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "mode_not_async"}))
		return nil
	}
	baseFields["config_version"] = cfg.ConfigVersion
	if !cfg.IncludesGroup(req.GroupID) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "group_out_of_scope"}))
		return nil
	}
	if len(cfg.EnabledEndpoints()) == 0 {
		e.recordDropped()
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "no_enabled_endpoint"}))
		return nil
	}
	snapshot, err := ExtractPromptSnapshot(req)
	if errors.Is(err, ErrNoPromptText) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "no_user_text"}))
		return nil
	}
	if err != nil {
		e.recordDropped()
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "snapshot_invalid"}))
		return nil
	}
	job, err := e.repo.CreateStagingWithCapacity(ctx, snapshot.Redacted(), cfg.ConfigVersion, 3, cfg.QueueCapacity)
	if err != nil {
		code := "database_unavailable"
		if errors.Is(err, ErrQueueFull) {
			code = "queue_full"
		}
		if errors.Is(err, ErrQueueAdmissionBusy) {
			code = "queue_admission_busy"
		}
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"queue_capacity": cfg.QueueCapacity, "status": "dropped", "error_code": code,
		}))
		e.recordDropped()
		return err
	}
	if err := e.payload.Set(ctx, job.ID, snapshot.ScanText, DefaultPayloadTTL); err != nil {
		_ = e.repo.MarkStagingFailed(ctx, job.ID, "payload_store_failed", "payload store unavailable")
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"job_id": job.ID, "status": "dropped", "error_code": "payload_store_failed",
		}))
		e.recordDropped()
		return err
	}
	if err := e.repo.PublishQueued(ctx, job.ID); err != nil {
		_ = e.payload.Delete(ctx, job.ID)
		_ = e.repo.MarkStagingFailed(ctx, job.ID, "queue_publish_failed", "queue publish failed")
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"job_id": job.ID, "status": "dropped", "error_code": "queue_publish_failed",
		}))
		e.recordDropped()
		return err
	}
	LogInfo(EventJobEnqueued, mergeLogFields(baseFields, map[string]any{
		"job_id":         job.ID,
		"queue_capacity": cfg.QueueCapacity, "status": "queued",
	}))
	if e.metrics != nil {
		e.metrics.IncEnqueued()
	}
	return nil
}

// Archive persists one request's textual context without entering the Prompt
// Guard queue. It is called only by PromptService's optional ArchiveRequest
// hook.
func (e *Enqueuer) Archive(ctx context.Context, req Request) error {
	if e == nil || e.repo == nil {
		return errors.New("prompt archive enqueuer unavailable")
	}
	archiveRepo, ok := e.repo.(ArchiveRepository)
	if !ok {
		return errors.New("prompt archive repository unavailable")
	}
	if isPromptArchiveHealthCheck(req.Body) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(archiveLogFields(req), map[string]any{
			"status": "skipped", "error_code": "health_check",
		}))
		return nil
	}
	archive, err := ExtractRequestContextArchive(req)
	if errors.Is(err, ErrNoPromptText) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(archiveLogFields(req), map[string]any{
			"status": "skipped", "error_code": "no_user_text",
		}))
		return nil
	}
	if err != nil {
		LogWarn(EventEnqueueDropped, mergeLogFields(archiveLogFields(req), map[string]any{
			"status": "dropped", "error_code": "archive_payload_invalid",
		}))
		return nil
	}
	if err := archiveRepo.RecordArchive(ctx, archive); err != nil {
		LogWarn(EventEnqueueDropped, mergeLogFields(archiveLogFields(req), map[string]any{
			"status": "dropped", "error_code": "archive_write_failed",
		}))
		return err
	}
	LogInfo(EventJobEnqueued, mergeLogFields(archiveLogFields(req), map[string]any{
		"status": "archived", "message_count": archive.MessageCount,
	}))
	return nil
}

// archiveLogFields deliberately excludes user, API-key, group, endpoint, and
// request-body data. Context archive logs are operational metadata only.
func archiveLogFields(req Request) map[string]any {
	return map[string]any{
		"request_id": req.RequestID,
		"protocol":   req.Protocol,
		"model":      req.Model,
		"stage":      normalizeStage(req.Stage),
	}
}

// isPromptArchiveHealthCheck excludes the known Claude Code connectivity
// probe (max_tokens=1 on a Haiku model). It should not create archive rows
// merely because a client is checking availability.
func isPromptArchiveHealthCheck(body []byte) bool {
	var request struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return false
	}
	return request.MaxTokens == 1 && strings.Contains(strings.ToLower(strings.TrimSpace(request.Model)), "haiku")
}

func (e *Enqueuer) recordDropped() {
	if e != nil && e.metrics != nil {
		e.metrics.IncDropped()
	}
}
