package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("shop.users")

var ErrNotFound = errors.New("user not found")

type Address struct {
	ID    string `json:"id"`
	Line1 string `json:"line1"`
	City  string `json:"city"`
}

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Addresses []Address `json:"addresses"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// GetUser loads a user and addresses.
//
// INTENTIONAL DEFECT (see DEFECTS.md): addresses are fetched with one query per
// row after listing IDs, instead of a single JOIN or WHERE user_id IN (...).
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	ctx, span := tracer.Start(ctx, "users.GetUser")
	defer span.End()
	span.SetAttributes(
		attribute.String("code.function.name", "Store.GetUser"),
		attribute.String("code.file.path", "examples/shop/internal/users/store.go"),
		attribute.String("user.id", id),
	)

	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, name FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `SELECT id FROM user_addresses WHERE user_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addrIDs []string
	for rows.Next() {
		var addrID string
		if err := rows.Scan(&addrID); err != nil {
			return nil, err
		}
		addrIDs = append(addrIDs, addrID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	u.Addresses = make([]Address, 0, len(addrIDs))
	for _, addrID := range addrIDs {
		var a Address
		err := s.pool.QueryRow(ctx,
			`SELECT id, line1, city FROM user_addresses WHERE id = $1`, addrID,
		).Scan(&a.ID, &a.Line1, &a.City)
		if err != nil {
			return nil, err
		}
		u.Addresses = append(u.Addresses, a)
	}
	return &u, nil
}
