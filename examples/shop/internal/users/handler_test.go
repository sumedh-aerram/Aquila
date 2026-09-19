package users

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct {
	user *User
	err  error
}

func (f fakeStore) GetUser(_ context.Context, id string) (*User, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.user == nil || f.user.ID != id {
		return nil, ErrNotFound
	}
	return f.user, nil
}

func (f fakeStore) Ping(context.Context) error { return nil }

func TestGetUserRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()
	h := NewHandler(fakeStore{user: &User{ID: "user-1", Email: "a@b.c", Name: "A"}})
	paths := []string{
		"/users/1%3BDROP%20TABLE%20users",
		"/users/../admin",
		"/users/user%20id",
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: expected rejection, got 200 body=%s", p, rec.Body.String())
		}
		if rec.Code >= 500 {
			t.Fatalf("%s: unsafe id must not 5xx, got %d", p, rec.Code)
		}
	}
}

func TestGetUserOK(t *testing.T) {
	t.Parallel()
	h := NewHandler(fakeStore{user: &User{ID: "user-1", Email: "ada@shop.test", Name: "Ada"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/user-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var u User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.Email != "ada@shop.test" {
		t.Fatalf("email=%q", u.Email)
	}
}

func TestGetUserNotFound(t *testing.T) {
	t.Parallel()
	h := NewHandler(fakeStore{err: ErrNotFound})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/user-missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestGetUserDoesNotLeakStoreError(t *testing.T) {
	t.Parallel()
	h := NewHandler(fakeStore{err: errors.New("password=super-secret connection refused")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/user-1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
	got := rec.Body.String()
	if strings.Contains(got, "super-secret") || strings.Contains(got, "password=") {
		t.Fatalf("leaked internals: %s", got)
	}
}
