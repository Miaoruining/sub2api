-- Dynamic pool quota snapshots and per-order/member window ledgers.
-- This migration deliberately leaves the historical migrations untouched.

ALTER TABLE pool_products DROP CONSTRAINT IF EXISTS pool_products_dynamic_quota_shape;
ALTER TABLE pool_orders DROP CONSTRAINT IF EXISTS pool_orders_dynamic_quota_shape;
ALTER TABLE pool_products DROP CONSTRAINT IF EXISTS pool_product_credit_config;
ALTER TABLE pool_orders DROP CONSTRAINT IF EXISTS pool_order_credit_config;

-- 240/242 used unnamed CHECK constraints for the token floor.  Replace only
-- those checks with a mode-aware form; named checks from this migration are
-- preserved when a development database re-runs the file.
DO $$
DECLARE c RECORD;
BEGIN
  FOR c IN
    SELECT conrelid::regclass AS rel, conname
    FROM pg_constraint
    WHERE contype='c'
      AND conrelid IN ('pool_products'::regclass,'pool_orders'::regclass)
      AND conname NOT IN ('pool_products_dynamic_quota_shape','pool_orders_dynamic_quota_shape')
      AND (pg_get_constraintdef(oid) LIKE '%total_tokens%' OR pg_get_constraintdef(oid) LIKE '%total_requests%')
  LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', c.rel, c.conname);
  END LOOP;
END $$;

DO $$
DECLARE c RECORD;
BEGIN
  FOR c IN
    SELECT conrelid::regclass AS rel, conname
    FROM pg_constraint
    WHERE contype='c'
      AND conrelid IN ('pool_products'::regclass,'pool_orders'::regclass)
      AND pg_get_constraintdef(oid) LIKE '%quota_mode%'
      AND conname NOT IN ('pool_products_dynamic_quota_shape','pool_orders_dynamic_quota_shape')
  LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', c.rel, c.conname);
  END LOOP;
END $$;

ALTER TABLE pool_products
  ADD CONSTRAINT pool_products_dynamic_quota_shape CHECK (
    (quota_mode='dynamic' AND total_tokens=0 AND total_requests=0
      AND total_credit=0 AND credit_5h=0 AND credit_7d=0)
    OR (quota_mode<>'dynamic' AND total_tokens>=seats*8192::BIGINT
      AND total_tokens<=1000000000000 AND total_requests>=seats
      AND total_requests<=1000000000)
  );
ALTER TABLE pool_orders
  ADD CONSTRAINT pool_orders_dynamic_quota_shape CHECK (
    (quota_mode='dynamic' AND total_tokens=0 AND total_requests=0
      AND total_credit=0 AND credit_5h=0 AND credit_7d=0)
    OR (quota_mode<>'dynamic' AND total_tokens>=seats
      AND total_tokens<=1000000000000 AND total_requests>=seats
      AND total_requests<=1000000000)
  );

-- 243's mode and credit checks are widened for the two dynamic modes.
ALTER TABLE pool_products ADD CONSTRAINT pool_products_quota_mode_allowed
  CHECK(quota_mode IN ('tokens','credits','dynamic','dynamic_shadow'));
ALTER TABLE pool_orders ADD CONSTRAINT pool_orders_quota_mode_allowed
  CHECK(quota_mode IN ('tokens','credits','dynamic','dynamic_shadow'));
ALTER TABLE pool_products ADD CONSTRAINT pool_product_credit_config CHECK(quota_mode IN ('tokens','dynamic') OR
  (total_credit>0 AND ((plan_type='plus' AND credit_5h>0 AND credit_7d>=credit_5h AND total_credit>=credit_7d) OR (plan_type='pro' AND credit_5h=0 AND credit_7d=0))));
ALTER TABLE pool_orders ADD CONSTRAINT pool_order_credit_config CHECK(quota_mode IN ('tokens','dynamic') OR
  (total_credit>0 AND ((plan_type='plus' AND credit_5h>0 AND credit_7d>=credit_5h AND total_credit>=credit_7d) OR (plan_type='pro' AND credit_5h=0 AND credit_7d=0))));

-- Every row is an immutable input event.  account_id + observed_at is the
-- caller supplied idempotency key; version makes ordering explicit.
CREATE TABLE IF NOT EXISTS pool_dynamic_snapshots (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL REFERENCES accounts(id),
  observed_at TIMESTAMPTZ NOT NULL,
  blocked BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(account_id, observed_at)
);
CREATE INDEX IF NOT EXISTS pool_dynamic_snapshots_latest
  ON pool_dynamic_snapshots(account_id, observed_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS pool_dynamic_snapshot_windows (
  snapshot_id BIGINT NOT NULL REFERENCES pool_dynamic_snapshots(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  used_percent NUMERIC(8,4) NOT NULL CHECK(used_percent>=0 AND used_percent<=100),
  reset_at TIMESTAMPTZ NOT NULL,
  window_seconds BIGINT NOT NULL CHECK(window_seconds>0),
  PRIMARY KEY(snapshot_id,key)
);

-- One generation per order/key/reset.  baseline_used is the amount already
-- consumed when the order was delivered; safety_percent is never allocated.
CREATE TABLE IF NOT EXISTS pool_dynamic_window_ledgers (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES pool_orders(id) ON DELETE CASCADE,
  account_id BIGINT NOT NULL REFERENCES accounts(id),
  key TEXT NOT NULL,
  reset_at TIMESTAMPTZ NOT NULL,
  window_seconds BIGINT NOT NULL CHECK(window_seconds>0),
  baseline_used_percent NUMERIC(8,4) NOT NULL CHECK(baseline_used_percent>=0 AND baseline_used_percent<=100),
  observed_used_percent NUMERIC(8,4) NOT NULL CHECK(observed_used_percent>=0 AND observed_used_percent<=100),
  external_used_percent NUMERIC(8,4) NOT NULL DEFAULT 0 CHECK(external_used_percent>=0 AND external_used_percent<=100),
  pending_delta_percent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(pending_delta_percent>=0),
  calibration_ratio NUMERIC(20,8) NOT NULL DEFAULT 10 CHECK(calibration_ratio>0),
  calibration_samples BIGINT NOT NULL DEFAULT 0 CHECK(calibration_samples>=0),
  observed_at TIMESTAMPTZ NOT NULL,
  snapshot_id BIGINT REFERENCES pool_dynamic_snapshots(id),
  safety_percent NUMERIC(8,4) NOT NULL DEFAULT 2 CHECK(safety_percent>=0 AND safety_percent<=100),
  status TEXT NOT NULL DEFAULT 'ready' CHECK(status IN ('ready','stale','unknown','exhausted','blocked')),
  UNIQUE(order_id,key,reset_at)
);
CREATE INDEX IF NOT EXISTS pool_dynamic_window_current
  ON pool_dynamic_window_ledgers(order_id,key,reset_at DESC);

CREATE TABLE IF NOT EXISTS pool_dynamic_member_ledgers (
  ledger_id BIGINT NOT NULL REFERENCES pool_dynamic_window_ledgers(id) ON DELETE CASCADE,
  member_id BIGINT NOT NULL REFERENCES pool_members(id) ON DELETE CASCADE,
  used_percent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(used_percent>=0),
  reserved_percent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(reserved_percent>=0),
  entitlement_percent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(entitlement_percent>=0),
  PRIMARY KEY(ledger_id,member_id)
);

CREATE TABLE IF NOT EXISTS pool_dynamic_request_windows (
  request_id UUID NOT NULL REFERENCES pool_requests(id) ON DELETE CASCADE,
  ledger_id BIGINT NOT NULL REFERENCES pool_dynamic_window_ledgers(id),
  key TEXT NOT NULL,
  reserved_percent NUMERIC(20,8) NOT NULL CHECK(reserved_percent>0),
  actual_percent NUMERIC(20,8),
  calibrated_snapshot_id BIGINT REFERENCES pool_dynamic_snapshots(id),
  calibration_status TEXT NOT NULL DEFAULT 'estimated' CHECK(calibration_status IN ('estimated','calibrated','uncertain')),
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','settled','uncertain','released')),
  PRIMARY KEY(request_id,key)
);
CREATE INDEX IF NOT EXISTS pool_dynamic_request_windows_ledger
  ON pool_dynamic_request_windows(ledger_id,status);

-- A short database lease prevents multiple service instances probing one
-- account at the same time.  It is independent from snapshot correctness.
CREATE TABLE IF NOT EXISTS pool_dynamic_refresh_leases (
  account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
  lease_until TIMESTAMPTZ NOT NULL,
  claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
