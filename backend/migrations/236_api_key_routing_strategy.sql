-- 仅扩展列；已有自动密钥默认使用智能策略，固定分组密钥行为不变。
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS routing_strategy VARCHAR(16) NOT NULL DEFAULT 'smart';
ALTER TABLE api_keys ADD CONSTRAINT api_keys_routing_strategy_check CHECK (routing_strategy IN ('smart', 'price', 'stable'));
