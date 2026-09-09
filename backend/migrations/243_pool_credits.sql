-- 老订单保持 Token 权益；新商品显式选择 Plus / Pro 的美元额度。
ALTER TABLE pool_products ADD COLUMN quota_mode TEXT NOT NULL DEFAULT 'tokens' CHECK(quota_mode IN ('tokens','credits')),
 ADD COLUMN plan_type TEXT NOT NULL DEFAULT 'plus' CHECK(plan_type IN ('plus','pro')),
 ADD COLUMN total_credit NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(total_credit>=0),
 ADD COLUMN credit_5h NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(credit_5h>=0),
 ADD COLUMN credit_7d NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(credit_7d>=0);
ALTER TABLE pool_orders ADD COLUMN quota_mode TEXT NOT NULL DEFAULT 'tokens' CHECK(quota_mode IN ('tokens','credits')),
 ADD COLUMN plan_type TEXT NOT NULL DEFAULT 'plus' CHECK(plan_type IN ('plus','pro')),
 ADD COLUMN total_credit NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(total_credit>=0),
 ADD COLUMN credit_5h NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(credit_5h>=0),
 ADD COLUMN credit_7d NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(credit_7d>=0);
ALTER TABLE pool_requests ADD COLUMN reserved_credit NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(reserved_credit>=0),
 ADD COLUMN actual_credit NUMERIC(20,8) CHECK(actual_credit>=0);
CREATE INDEX pool_requests_member_time ON pool_requests(member_id,created_at);
-- 使用记录直接按不可变的专属 Key 关联订单，包含历史记录，无需改写大表。

ALTER TABLE pool_products ADD CONSTRAINT pool_product_credit_config CHECK(quota_mode='tokens' OR
 (total_credit>0 AND ((plan_type='plus' AND credit_5h>0 AND credit_7d>=credit_5h AND total_credit>=credit_7d) OR (plan_type='pro' AND credit_5h=0 AND credit_7d=0))));
ALTER TABLE pool_orders ADD CONSTRAINT pool_order_credit_config CHECK(quota_mode='tokens' OR
 (total_credit>0 AND ((plan_type='plus' AND credit_5h>0 AND credit_7d>=credit_5h AND total_credit>=credit_7d) OR (plan_type='pro' AND credit_5h=0 AND credit_7d=0))));
