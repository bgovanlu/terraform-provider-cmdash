// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDo_SendsBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "cmdash_ek_test123")
	if err := c.Do(context.Background(), http.MethodGet, "/v2/webhooks", "", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotAuth != "Bearer cmdash_ek_test123" {
		t.Fatalf("expected Bearer auth header, got %q", gotAuth)
	}
}

func TestDo_SendsIdempotencyKeyOnWritesOnly(t *testing.T) {
	var gotKeyOnPost, gotKeyOnGet string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotKeyOnPost = r.Header.Get("Idempotency-Key")
		} else {
			gotKeyOnGet = r.Header.Get("Idempotency-Key")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	if err := c.Do(context.Background(), http.MethodPost, "/v2/webhooks", "abc12345", map[string]string{"url": "https://x"}, nil); err != nil {
		t.Fatalf("Do POST: %v", err)
	}
	if err := c.Do(context.Background(), http.MethodGet, "/v2/webhooks", "abc12345", nil, nil); err != nil {
		t.Fatalf("Do GET: %v", err)
	}

	if gotKeyOnPost != "abc12345" {
		t.Errorf("expected Idempotency-Key on POST, got %q", gotKeyOnPost)
	}
	if gotKeyOnGet != "" {
		// The interceptor only inspects the header for non-GET/HEAD methods
		// anyway (idempotency.interceptor.ts IDEMPOTENT_METHODS), but this
		// client should not even send it on a GET.
		t.Errorf("expected NO Idempotency-Key on GET, got %q", gotKeyOnGet)
	}
}

func TestDo_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("X-RateLimit-Reset", "1") // 1 unix second — already in the past, so backoffFor caps/short-circuits
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"statusCode":429,"message":"slow down"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"requestId":"req-1","timestamp":"2026-10-09T12:00:00.000Z","data":{"id":"ok"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	var out struct {
		ID string `json:"id"`
	}
	start := time.Now()
	if err := c.Do(context.Background(), http.MethodGet, "/v2/webhooks", "", nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("retry took too long — backoff not honoring a stale X-RateLimit-Reset")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected exactly 2 calls (1 failed + 1 retry), got %d", calls)
	}
	if out.ID != "ok" {
		t.Fatalf("expected decoded body, got %+v", out)
	}
}

func TestDo_GivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"statusCode":429,"message":"nope"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	err := c.Do(context.Background(), http.MethodGet, "/v2/webhooks", "", nil, nil)
	if err == nil {
		t.Fatal("expected an error after exhausting retries, got nil")
	}
	// maxRetries=5 means attempts 0..5 inclusive = 6 calls total.
	if atomic.LoadInt32(&calls) != maxRetries+1 {
		t.Fatalf("expected %d calls, got %d", maxRetries+1, calls)
	}
}

func TestDo_4xxIsNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"statusCode":404,"message":"not found"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	err := c.Do(context.Background(), http.MethodGet, "/v2/webhooks/missing", "", nil, nil)
	if err == nil {
		t.Fatal("expected a 404 error")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound(err) == true, got err=%v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 call — a 404 must NOT be retried like a 429, got %d", calls)
	}
}

func TestNewIdempotencyKey_MatchesServerPattern(t *testing.T) {
	// Mirrors apps/api/src/idempotency/idempotency.interceptor.ts's
	// KEY_PATTERN = /^[A-Za-z0-9._-]{8,128}$/.
	k := NewIdempotencyKey("webhook-create")
	if len(k) < 8 || len(k) > 128 {
		t.Fatalf("key length %d out of [8,128]: %q", len(k), k)
	}
	for _, r := range k {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			t.Fatalf("key %q contains char %q outside [A-Za-z0-9._-]", k, r)
		}
	}
}
