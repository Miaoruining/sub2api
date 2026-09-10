-- Subscription quota packages: immutable plan/order snapshots and per-order grants.

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS plan_kind VARCHAR(20) NOT NULL DEFAULT 'base',
    ADD COLUMN IF NOT EXISTS quota_usd DECIMAL(20, 8),
    ADD COLUMN IF NOT EXISTS allow_active_renewal BOOLEAN NOT NULL DEFAULT FALSE;

-- Plans created before quota packages keep the historical renewal behavior.
-- New plans use the schema default (FALSE), so a 30-day product cannot extend
-- an already-active cycle unless the administrator explicitly enables it.
UPDATE subscription_plans
SET allow_active_renewal = TRUE
WHERE plan_kind = 'base' AND allow_active_renewal = FALSE;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT,
    ADD COLUMN IF NOT EXISTS subscription_cycle_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS quota_usd DECIMAL(20, 8),
    ADD COLUMN IF NOT EXISTS subscription_allow_active_renewal BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS subscription_quota_grants (
    id                  BIGSERIAL PRIMARY KEY,
    subscription_id     BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE CASCADE,
    payment_order_id    BIGINT NOT NULL UNIQUE REFERENCES payment_orders(id) ON DELETE CASCADE,
    cycle_start         TIMESTAMPTZ NOT NULL,
    granted_usd         DECIMAL(20, 8) NOT NULL,
    used_usd            DECIMAL(20, 8) NOT NULL DEFAULT 0,
    status              VARCHAR(32) NOT NULL DEFAULT 'active',
    refunded_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Keep this migration safe if a candidate process created the table before a
-- retry with the finalized status vocabulary (consumption_ambiguous is 21 chars).
ALTER TABLE subscription_quota_grants
    ALTER COLUMN status TYPE VARCHAR(32);

CREATE INDEX IF NOT EXISTS idx_subscription_quota_grants_cycle
    ON subscription_quota_grants(subscription_id, cycle_start, status);

-- A stale in-flight request must not be charged to a new subscription cycle,
-- but its billable evidence must remain available for reconciliation.
CREATE TABLE IF NOT EXISTS subscription_billing_failures (
    id                  BIGSERIAL PRIMARY KEY,
    request_id          TEXT NOT NULL,
    api_key_id          BIGINT NOT NULL,
    subscription_id     BIGINT NOT NULL,
    cycle_snapshot      TIMESTAMPTZ NOT NULL,
    command             JSONB NOT NULL,
    reason              TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(request_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_subscription_billing_failures_cycle
    ON subscription_billing_failures(subscription_id, cycle_snapshot);
