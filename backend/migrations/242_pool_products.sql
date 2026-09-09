-- 商品长期展示；用户首笔付款才创建订单，订单保留购买时的条款快照。
CREATE TABLE pool_products (
 id BIGSERIAL PRIMARY KEY, title VARCHAR(100) NOT NULL, description TEXT NOT NULL DEFAULT '',
 seats INTEGER NOT NULL CHECK(seats BETWEEN 2 AND 50), price NUMERIC(20,8) NOT NULL CHECK(price>0),
 duration_days INTEGER NOT NULL CHECK(duration_days BETWEEN 1 AND 365),
 formation_days INTEGER NOT NULL CHECK(formation_days BETWEEN 1 AND 90),
 total_tokens BIGINT NOT NULL, total_requests BIGINT NOT NULL, concurrency INTEGER NOT NULL CHECK(concurrency BETWEEN 1 AND 10),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 version BIGINT NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(total_tokens>=seats*8192::BIGINT AND total_tokens<=1000000000000 AND total_requests>=seats AND total_requests<=1000000000)
);
ALTER TABLE pool_orders ADD COLUMN product_id BIGINT REFERENCES pool_products(id);
CREATE INDEX pool_orders_product_id ON pool_orders(product_id);
CREATE TABLE pool_product_purchases (
 user_id BIGINT NOT NULL REFERENCES users(id), request_id UUID NOT NULL,
 product_id BIGINT NOT NULL REFERENCES pool_products(id), order_id BIGINT NOT NULL REFERENCES pool_orders(id),
 PRIMARY KEY(user_id,request_id)
);
