-- 成团后发货；已开通历史订单保持原有效期。
ALTER TABLE pool_orders ALTER COLUMN group_id DROP NOT NULL;
ALTER TABLE pool_orders DROP CONSTRAINT pool_orders_status_check;
ALTER TABLE pool_orders ADD CONSTRAINT pool_orders_status_check CHECK(status IN ('forming','awaiting_delivery','active','cancelled'));
ALTER TABLE pool_orders ADD COLUMN formed_at TIMESTAMPTZ;
ALTER TABLE pool_orders ADD COLUMN delivery_deadline TIMESTAMPTZ;
ALTER TABLE pool_orders ADD COLUMN resource_id BIGINT REFERENCES pool_resources(id);
UPDATE pool_orders p SET resource_id=r.id FROM pool_resources r WHERE p.group_id=r.group_id;
UPDATE pool_orders SET formed_at=starts_at WHERE status='active';
-- 老版本未成团订单释放预绑定，统一走发货流程。
UPDATE pool_orders SET group_id=NULL,resource_id=NULL WHERE status='forming';
CREATE TABLE pool_notifications (
 id BIGSERIAL PRIMARY KEY, order_id BIGINT NOT NULL REFERENCES pool_orders(id),
 user_id BIGINT REFERENCES users(id), kind TEXT NOT NULL CHECK(kind IN ('delivery_required','delivered')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX pool_notifications_event ON pool_notifications(order_id,kind,COALESCE(user_id,0));
CREATE TABLE pool_notification_reads (
 notification_id BIGINT NOT NULL REFERENCES pool_notifications(id), user_id BIGINT NOT NULL REFERENCES users(id),
 PRIMARY KEY(notification_id,user_id)
);
-- 普通编辑/批量编辑不能摘除独立号池的分组，事务内轮换分组仍合法。
CREATE FUNCTION protect_pool_membership_deletion() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM pool_resources r JOIN accounts a ON a.id=r.account_id
 WHERE r.account_id=OLD.account_id AND a.deleted_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM account_groups ag WHERE ag.account_id=r.account_id AND ag.group_id=r.group_id)) THEN
 RAISE EXCEPTION '拼单账号分组由订单发货维护，不能移除';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER protect_pool_membership_deletion AFTER DELETE ON account_groups DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION protect_pool_membership_deletion();
CREATE FUNCTION protect_pool_account_type() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM pool_resources WHERE account_id=OLD.id) AND (NEW.platform<>'openai' OR NEW.type NOT IN ('oauth','apikey')) THEN
 RAISE EXCEPTION '拼单账号必须保持 OpenAI OAuth 或 API Key 类型';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_pool_account_type BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION protect_pool_account_type();
