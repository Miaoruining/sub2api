-- Optional source group for composite routes.  The source must be a public,
-- standard balance group; service validation enforces the runtime policy.
ALTER TABLE composite_model_routes
    ADD COLUMN IF NOT EXISTS source_group_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'composite_model_routes_source_group_id_fkey'
    ) THEN
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_source_group_id_fkey
            FOREIGN KEY (source_group_id) REFERENCES groups(id) ON DELETE RESTRICT;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'composite_model_routes_source_group_not_self_check'
    ) THEN
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_source_group_not_self_check
            CHECK (source_group_id IS NULL OR source_group_id <> group_id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_composite_model_routes_source_group
    ON composite_model_routes (source_group_id)
    WHERE deleted_at IS NULL;
