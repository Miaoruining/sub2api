-- 管理员独立参与开关：默认开启，但仍共享每日真实发放预算。
ALTER TABLE lottery_settings ADD COLUMN admin_repeat_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE lottery_draws ADD COLUMN admin_repeat BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE lottery_draws ADD COLUMN request_id UUID;
ALTER TABLE lottery_draws ADD CONSTRAINT lottery_admin_request_required CHECK (NOT admin_repeat OR request_id IS NOT NULL);

-- 保留所有历史流水，普通用户仍受每日唯一约束。
ALTER TABLE lottery_draws DROP CONSTRAINT lottery_draws_user_id_activity_date_key;
CREATE UNIQUE INDEX lottery_draws_daily_regular_unique ON lottery_draws(user_id, activity_date) WHERE NOT admin_repeat;
CREATE UNIQUE INDEX lottery_draws_request_unique ON lottery_draws(user_id, activity_date, request_id) WHERE request_id IS NOT NULL;
