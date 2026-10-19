-- Keep demo stock levels aligned with migrations/000001_init.up.sql.
BEGIN;

INSERT INTO items (sku, name, price_cents, stock) VALUES
    ('sku-widget', 'Widget', 2500, 100),
    ('sku-gadget', 'Gadget', 4900, 50)
ON CONFLICT (sku) DO UPDATE SET stock = EXCLUDED.stock;

COMMIT;
