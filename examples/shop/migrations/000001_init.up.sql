BEGIN;

CREATE TABLE users (
    id    TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    name  TEXT NOT NULL
);

CREATE TABLE user_addresses (
    id      TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users (id),
    line1   TEXT NOT NULL,
    city    TEXT NOT NULL
);
-- INTENTIONAL: no index on user_addresses.user_id (see DEFECTS.md).

CREATE TABLE items (
    sku         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    price_cents INTEGER NOT NULL CHECK (price_cents >= 0),
    stock       INTEGER NOT NULL CHECK (stock >= 0)
);

CREATE TABLE inventory_events (
    id          BIGSERIAL PRIMARY KEY,
    sku         TEXT NOT NULL,
    checkout_id TEXT NOT NULL,
    kind        TEXT NOT NULL,
    qty         INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- INTENTIONAL: no index on sku or checkout_id.

CREATE TABLE inventory_mutex (
    id        INTEGER PRIMARY KEY,
    locked_at TIMESTAMPTZ
);
INSERT INTO inventory_mutex (id) VALUES (1);

CREATE TABLE checkouts (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    status      TEXT NOT NULL,
    total_cents INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE checkout_items (
    checkout_id TEXT NOT NULL REFERENCES checkouts (id),
    sku         TEXT NOT NULL,
    qty         INTEGER NOT NULL,
    price_cents INTEGER NOT NULL
);

CREATE TABLE payments (
    id           TEXT PRIMARY KEY,
    checkout_id  TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    amount_cents INTEGER NOT NULL,
    status       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO users (id, email, name) VALUES
    ('user-1', 'ada@shop.test', 'Ada Lovelace'),
    ('user-2', 'grace@shop.test', 'Grace Hopper');

INSERT INTO user_addresses (id, user_id, line1, city) VALUES
    ('addr-1', 'user-1', '1 Analytical Engine Way', 'London'),
    ('addr-2', 'user-1', '2 Binary Court', 'London'),
    ('addr-3', 'user-2', '3 Compiler Row', 'New York');

INSERT INTO items (sku, name, price_cents, stock) VALUES
    ('sku-widget', 'Widget', 2500, 100),
    ('sku-gadget', 'Gadget', 4900, 50);

COMMIT;
