-- 抽奖奖励与支付订单隔离，不能被视为真实充值或触发邀请返利。
CREATE TABLE lottery_settings (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE
);
INSERT INTO lottery_settings (id) VALUES (1);

CREATE TABLE lottery_rules (
    effective_date DATE PRIMARY KEY,
    weights JSONB NOT NULL CHECK (jsonb_typeof(weights) = 'array' AND jsonb_array_length(weights) = 7),
    updated_by BIGINT REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO lottery_rules (effective_date, weights)
VALUES ('1970-01-01', '[9552,400,30,10,5,2,1]');

CREATE TABLE lottery_days (
    activity_date DATE PRIMARY KEY,
    spent INTEGER NOT NULL DEFAULT 0 CHECK (spent BETWEEN 0 AND 100),
    draw_count INTEGER NOT NULL DEFAULT 0 CHECK (draw_count >= 0),
    weights JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同时作为不可重复的抽奖结果和奖励余额流水。零奖也必须留记录。
CREATE TABLE lottery_draws (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    activity_date DATE NOT NULL REFERENCES lottery_days(activity_date),
    prize INTEGER NOT NULL CHECK (prize IN (0,1,5,10,20,50,100)),
    ticket INTEGER NOT NULL CHECK (ticket BETWEEN 0 AND 9999),
    budget_before INTEGER NOT NULL CHECK (budget_before BETWEEN 1 AND 100),
    balance_before NUMERIC(20,8) NOT NULL,
    balance_after NUMERIC(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, activity_date),
    CHECK (prize <= budget_before),
    CHECK (balance_after = balance_before + prize)
);
CREATE INDEX lottery_draws_day_id_idx ON lottery_draws(activity_date, id DESC);
CREATE INDEX lottery_draws_user_id_idx ON lottery_draws(user_id, id DESC);

CREATE TABLE lottery_config_audits (
    id BIGSERIAL PRIMARY KEY,
    actor_id BIGINT NOT NULL REFERENCES users(id),
    before_config JSONB NOT NULL,
    after_config JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
