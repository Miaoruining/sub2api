-- 独立拼单号池：独立入口维护；底层复用 OAuth 刷新与协议转发。
CREATE TABLE pool_resources (
 id BIGSERIAL PRIMARY KEY,
 account_id BIGINT NOT NULL UNIQUE REFERENCES accounts(id),
 group_id BIGINT NOT NULL UNIQUE REFERENCES groups(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE FUNCTION protect_pool_account_group() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF (EXISTS(SELECT 1 FROM pool_resources WHERE account_id=NEW.account_id OR group_id=NEW.group_id) OR EXISTS(SELECT 1 FROM pool_orders WHERE group_id=NEW.group_id))
 AND NOT EXISTS(SELECT 1 FROM pool_resources WHERE account_id=NEW.account_id AND group_id=NEW.group_id) THEN
  RAISE EXCEPTION '拼单账号与普通号池不能混用';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_pool_account_group BEFORE INSERT OR UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION protect_pool_account_group();
CREATE FUNCTION protect_pool_group() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF (EXISTS(SELECT 1 FROM pool_resources WHERE group_id=OLD.id) OR EXISTS(SELECT 1 FROM pool_orders WHERE group_id=OLD.id)) AND
 (NEW.platform<>'openai' OR NEW.subscription_type<>'subscription' OR NOT NEW.is_exclusive
 OR NEW.fallback_group_id IS NOT NULL OR NEW.fallback_group_id_on_invalid_request IS NOT NULL) THEN
  RAISE EXCEPTION '拼单资源组不允许开启普通号池回退或更改订阅类型';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_pool_group BEFORE UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION protect_pool_group();
-- 拼单按订单锁定人数、价格和额度；钱包扣款与席位在同一事务提交。
CREATE TABLE pool_orders (
 id BIGSERIAL PRIMARY KEY,
 title VARCHAR(100) NOT NULL,
 group_id BIGINT NOT NULL UNIQUE REFERENCES groups(id),
 seats INTEGER NOT NULL CHECK(seats BETWEEN 2 AND 50),
 price NUMERIC(20,8) NOT NULL CHECK(price > 0),
 duration_hours INTEGER NOT NULL CHECK(duration_hours BETWEEN 1 AND 8760),
 total_tokens BIGINT NOT NULL CHECK(total_tokens BETWEEN 1 AND 1000000000000),
 total_requests BIGINT NOT NULL CHECK(total_requests BETWEEN 1 AND 1000000000),
 concurrency INTEGER NOT NULL CHECK(concurrency BETWEEN 1 AND 10),
 status TEXT NOT NULL DEFAULT 'forming' CHECK(status IN ('forming','active','cancelled')),
 join_deadline TIMESTAMPTZ NOT NULL,
 starts_at TIMESTAMPTZ,
 expires_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(total_tokens >= seats AND total_requests >= seats)
);
CREATE TABLE pool_members (
 id BIGSERIAL PRIMARY KEY,
 order_id BIGINT NOT NULL REFERENCES pool_orders(id),
 user_id BIGINT NOT NULL REFERENCES users(id),
 api_key_id BIGINT UNIQUE REFERENCES api_keys(id),
 status TEXT NOT NULL DEFAULT 'joined' CHECK(status IN ('joined','refunded')),
 paid NUMERIC(20,8) NOT NULL,
 refunded NUMERIC(20,8) NOT NULL DEFAULT 0,
 tokens_used BIGINT NOT NULL DEFAULT 0 CHECK(tokens_used >= 0),
 requests_used BIGINT NOT NULL DEFAULT 0 CHECK(requests_used >= 0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(order_id,user_id)
);
CREATE TABLE pool_requests (
 id UUID PRIMARY KEY,
 member_id BIGINT NOT NULL REFERENCES pool_members(id),
 reserved_tokens BIGINT NOT NULL CHECK(reserved_tokens > 0),
 actual_tokens BIGINT,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','settled','released','uncertain')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 finished_at TIMESTAMPTZ
);
CREATE INDEX pool_requests_pending ON pool_requests(member_id) WHERE status='pending';
CREATE TABLE pool_wallet_entries (
 id BIGSERIAL PRIMARY KEY,
 member_id BIGINT NOT NULL REFERENCES pool_members(id),
 kind TEXT NOT NULL CHECK(kind IN ('purchase','refund')),
 amount NUMERIC(20,8) NOT NULL,
 balance_after NUMERIC(20,8) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(member_id,kind)
);
-- 防止修改专属 Key 的归属或改组绕开拼单计量。允许改名、停用和常规计费更新。
CREATE FUNCTION protect_pool_key() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM pool_members WHERE api_key_id=OLD.id) AND
 (NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.group_id IS DISTINCT FROM OLD.group_id OR NEW.auto_group) THEN
  RAISE EXCEPTION '拼单专属 Key 不允许修改用户、分组或开启自动路由';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_pool_key BEFORE UPDATE ON api_keys FOR EACH ROW EXECUTE FUNCTION protect_pool_key();

CREATE FUNCTION notify_pool_resource() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO scheduler_outbox(event_type,account_id) VALUES('account_changed',NEW.account_id);
 RETURN NEW;
END $$;
CREATE TRIGGER notify_pool_resource AFTER INSERT OR UPDATE ON pool_resources FOR EACH ROW EXECUTE FUNCTION notify_pool_resource();
CREATE FUNCTION notify_pool_account_status() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM pool_resources WHERE account_id=NEW.id) AND NEW.status IS DISTINCT FROM OLD.status THEN
 INSERT INTO scheduler_outbox(event_type,account_id) VALUES('account_changed',NEW.id);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER notify_pool_account_status AFTER UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION notify_pool_account_status();
