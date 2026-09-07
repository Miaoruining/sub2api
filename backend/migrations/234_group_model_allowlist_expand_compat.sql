-- 在官方 235_group_model_allowlist.sql 之前先做 expand 阶段迁移。
-- 滚动发布期间旧实例继续读取 models_list_config，新实例读取 model_allowlist，
-- 避免候选实例启动迁移后立即破坏仍在承载流量的 v0.2.1 实例。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'groups'
          AND column_name = 'model_allowlist'
    ) THEN
        ALTER TABLE groups
            ADD COLUMN model_allowlist jsonb NOT NULL
            DEFAULT '{"enabled":false,"models":[]}'::jsonb;

        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'groups'
              AND column_name = 'models_list_config'
        ) THEN
            UPDATE groups
            SET model_allowlist = models_list_config
            WHERE models_list_config IS NOT NULL;
        END IF;
    END IF;
END
$$;
