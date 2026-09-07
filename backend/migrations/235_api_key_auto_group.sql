-- 默认关闭，存量密钥保持原有分组和计费行为。
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS auto_group BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_auto_group_assignment
    CHECK (NOT auto_group OR group_id IS NULL);
