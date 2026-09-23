package rewrite

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

const (
	d2File = "internal/users/store.go"
	d2Old  = "	rows, err := s.pool.Query(ctx, `SELECT id FROM user_addresses WHERE user_id = $1`, id)\n" +
		"	if err != nil {\n" +
		"		return nil, err\n" +
		"	}\n" +
		"	defer rows.Close()\n" +
		"\n" +
		"	var addrIDs []string\n" +
		"	for rows.Next() {\n" +
		"		var addrID string\n" +
		"		if err := rows.Scan(&addrID); err != nil {\n" +
		"			return nil, err\n" +
		"		}\n" +
		"		addrIDs = append(addrIDs, addrID)\n" +
		"	}\n" +
		"	if err := rows.Err(); err != nil {\n" +
		"		return nil, err\n" +
		"	}\n" +
		"\n" +
		"	u.Addresses = make([]Address, 0, len(addrIDs))\n" +
		"	for _, addrID := range addrIDs {\n" +
		"		var a Address\n" +
		"		err := s.pool.QueryRow(ctx,\n" +
		"			`SELECT id, line1, city FROM user_addresses WHERE id = $1`, addrID,\n" +
		"		).Scan(&a.ID, &a.Line1, &a.City)\n" +
		"		if err != nil {\n" +
		"			return nil, err\n" +
		"		}\n" +
		"		u.Addresses = append(u.Addresses, a)\n" +
		"	}\n"
	d2New = "	rows, err := s.pool.Query(ctx, `SELECT id, line1, city FROM user_addresses WHERE user_id = $1`, id)\n" +
		"	if err != nil {\n" +
		"		return nil, err\n" +
		"	}\n" +
		"	defer rows.Close()\n" +
		"	for rows.Next() {\n" +
		"		var a Address\n" +
		"		if err := rows.Scan(&a.ID, &a.Line1, &a.City); err != nil {\n" +
		"			return nil, err\n" +
		"		}\n" +
		"		u.Addresses = append(u.Addresses, a)\n" +
		"	}\n" +
		"	if err := rows.Err(); err != nil {\n" +
		"		return nil, err\n" +
		"	}\n"

	d3File = "migrations/000001_init.up.sql"
	d3Old  = "CREATE TABLE inventory_events (\n" +
		"    id          BIGSERIAL PRIMARY KEY,\n" +
		"    sku         TEXT NOT NULL,\n" +
		"    checkout_id TEXT NOT NULL,\n" +
		"    kind        TEXT NOT NULL,\n" +
		"    qty         INTEGER NOT NULL,\n" +
		"    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()\n" +
		");\n"
	d3New = "CREATE TABLE inventory_events (\n" +
		"    id          BIGSERIAL PRIMARY KEY,\n" +
		"    sku         TEXT NOT NULL,\n" +
		"    checkout_id TEXT NOT NULL,\n" +
		"    kind        TEXT NOT NULL,\n" +
		"    qty         INTEGER NOT NULL,\n" +
		"    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()\n" +
		");\n" +
		"CREATE INDEX inventory_events_sku ON inventory_events (sku);\n"

	d4File = "internal/checkout/handler.go"
	d4Old  = "	for range 5 {\n"
	d4New  = "	for range 1 {\n"

	d5File = "internal/checkout/handler.go"
	d5Old  = "	_ = svcclient.PostJSON(ctx, h.http, h.notificationURL+\"/notify\", map[string]any{\n" +
		"		\"user_id\": req.UserID, \"checkout_id\": checkoutID, \"kind\": \"order_confirmed\",\n" +
		"	}, nil)\n"
	d5New = "	go func() {\n" +
		"		_ = svcclient.PostJSON(context.Background(), h.http, h.notificationURL+\"/notify\", map[string]any{\n" +
		"			\"user_id\": req.UserID, \"checkout_id\": checkoutID, \"kind\": \"order_confirmed\",\n" +
		"		}, nil)\n" +
		"	}()\n"

	d6File = "internal/inventory/handler.go"
	d6Old  = "	ok, err := h.rdb.SetNX(ctx, \"inventory:global\", req.CheckoutID, 3*time.Second).Result()\n" +
		"	if err != nil {\n" +
		"		httputil.WriteError(w, http.StatusServiceUnavailable, \"lock unavailable\")\n" +
		"		return\n" +
		"	}\n" +
		"	if !ok {\n" +
		"		httputil.WriteError(w, http.StatusConflict, \"inventory busy\")\n" +
		"		return\n" +
		"	}\n" +
		"	defer func() { _ = h.rdb.Del(context.Background(), \"inventory:global\").Err() }()\n"
	d6New = "	lockKey := \"inventory:\" + req.SKU\n" +
		"	ok, err := h.rdb.SetNX(ctx, lockKey, req.CheckoutID, 3*time.Second).Result()\n" +
		"	if err != nil {\n" +
		"		httputil.WriteError(w, http.StatusServiceUnavailable, \"lock unavailable\")\n" +
		"		return\n" +
		"	}\n" +
		"	if !ok {\n" +
		"		httputil.WriteError(w, http.StatusConflict, \"inventory busy\")\n" +
		"		return\n" +
		"	}\n" +
		"	defer func() { _ = h.rdb.Del(context.Background(), lockKey).Err() }()\n"
)

// Candidates returns the first shop rewrite that matches files. It does not
// write the module tree and does not claim the change is safe.
func Candidates(dir string, files []string) ([]byte, error) {
	for _, fn := range []func(string, []string) ([]byte, error){D1, D2, D4, D5, D6, D3} {
		out, err := fn(dir, files)
		if err == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("rewrite: no candidate")
}

// D2 returns a unified diff that loads user addresses in one query.
func D2(dir string, files []string) ([]byte, error) {
	return candidate(dir, files, d2File, d2Old, d2New)
}

// D3 returns a unified diff that indexes inventory_events.sku.
func D3(dir string, files []string) ([]byte, error) {
	return candidate(dir, files, d3File, d3Old, d3New)
}

// D4 returns a unified diff that stops checkout payment retry amplification.
func D4(dir string, files []string) ([]byte, error) {
	return candidate(dir, files, d4File, d4Old, d4New)
}

// D5 returns a unified diff that moves notify off the checkout critical path.
func D5(dir string, files []string) ([]byte, error) {
	return candidate(dir, files, d5File, d5Old, d5New)
}

// D6 returns a unified diff that scopes the inventory Redis lock per SKU.
func D6(dir string, files []string) ([]byte, error) {
	return candidate(dir, files, d6File, d6Old, d6New)
}

func candidate(dir string, files []string, rel, oldText, newText string) ([]byte, error) {
	if !touches(files, rel) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("rewrite: %w", err)
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rewrite: %w", err)
	}
	if !bytes.Contains(raw, []byte(oldText)) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	if bytes.Contains(raw, []byte(newText)) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	idx := bytes.Index(raw, []byte(oldText))
	if idx < 0 {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	start := 1 + bytes.Count(raw[:idx], []byte("\n"))
	return unified(rel, start, oldText, newText), nil
}
