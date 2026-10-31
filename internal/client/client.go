// SPDX-License-Identifier: Apache-2.0

// Package client is a thin HTTP wrapper around CMDash's public API v2
// (apps/api/src/public-api/v2/**). ROADMAP-V70 E31.
//
// Contract verified at source, not assumed:
//   - Auth: `Authorization: Bearer cmdash_ek_...` (apps/api/src/public-api/api-key.guard.ts).
//   - Rate limiting: per-API-key, 1hr fixed window, tier-resolved
//     (500/2000/10000/20000 req/hr). Every response carries
//     X-RateLimit-Limit/Remaining/Reset; 429 carries the standard error
//     envelope (apps/api/src/public-api/api-rate-limit.guard.ts,
//     docs/developers/public-api-versioning.md "Rate limits"). This client
//     retries a 429 with exponential backoff seeded from Retry-After / the
//     reset time, not a fixed guess.
//   - Idempotency: writes (POST/PUT/PATCH/DELETE) may carry an
//     `Idempotency-Key` header (apps/api/src/idempotency/idempotency.interceptor.ts).
//     Since 2026-09-03 this is correctly tenant-scoped even under API-key
//     auth (see that file's doc comment on the prior cross-tenant
//     collision bug) — safe to send unconditionally from Terraform, where
//     retrying the exact same write with the exact same key is precisely
//     the "did my last apply actually land" question this header answers.
//     This client sends a fresh key per logical operation (derived from
//     method+path+a random nonce) so a genuine retry of the SAME operation
//     reuses it, while two different resource operations never collide.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	// DefaultBaseURL is the production product API (apps/api's public-api
	// v1/v2 routes), NOT the control plane (api.cmdash.com is
	// apps/control-plane — catalog, payments, licensing, provisioning — a
	// wholly separate deploy with zero v1/v2 surface; every request built
	// against that host 404s). Overridable via the provider's base_url
	// attribute or CMDASH_BASE_URL. See #583 and packages/sdk-js/src/
	// index.ts's DEFAULT_BASE_URL, which already gets this right.
	DefaultBaseURL = "https://app.cmdash.com/api"

	maxRetries     = 5
	defaultBackoff = 250 * time.Millisecond
	maxBackoff     = 30 * time.Second
)

// Client is a minimal, dependency-free HTTP client for /v2/* routes.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// New builds a Client. baseURL should already include the /api prefix
// (e.g. "https://app.cmdash.com/api" or "http://127.0.0.1:PORT/api" in
// tests) — /v2/... is appended by callers.
func New(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError mirrors the standard error envelope every v2 route returns on
// failure (statusCode/error/message — see ApiErrorResponseDto and
// ApiRateLimitGuard's 429 body in apps/api/src/public-api).
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cmdash API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// IsNotFound reports whether err is a 404 from the API — the signal every
// resource's Read uses to remove an object from state (import support and
// drift detection both rely on this).
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// envelope is the success wrapper ResponseInterceptor puts around EVERY v2
// response: {"success":true,"requestId":"...","timestamp":"...","data":<payload>}
// (apps/api/src/common/interceptors/response.interceptor.ts; the shared
// definition is packages/shared/src/envelope/index.ts, and the wire bodies
// the provider's tests use are packages/shared/src/envelope/
// envelope-contract.fixture.json, proven against the real interceptor by
// apps/api/test/client-envelope-contract-a1009-p1.spec.ts).
//
// This client used to unmarshal the raw body into the payload type, so every
// call against the real API either failed to decode (ListWebhooks wants an
// array, gets an object) or silently produced a zero value (a created webhook
// with an empty ID). Audit 2026-10-09 API-1.
type envelope struct {
	Success   *bool           `json:"success"`
	RequestID *string         `json:"requestId"`
	Timestamp *string         `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// UnwrapEnvelope returns the "data" payload of a success envelope. A body that
// is not the envelope, or has no "data", is an ERROR, never a zero value: a
// provider that quietly decodes nothing would plan against an empty tenant.
func UnwrapEnvelope(body []byte) (json.RawMessage, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode response body as the API envelope: %w", err)
	}
	if env.Success == nil || !*env.Success || env.RequestID == nil || env.Timestamp == nil {
		return nil, fmt.Errorf("response is not the {success, requestId, timestamp, data} envelope: %s", truncate(body))
	}
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("response envelope has no \"data\" field (requestId %s)", *env.RequestID)
	}
	return env.Data, nil
}

func truncate(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// NewIdempotencyKey returns a fresh key matching the interceptor's pattern
// `[A-Za-z0-9._-]{8,128}` (apps/api/src/idempotency/idempotency.interceptor.ts).
// Callers that want retries of the SAME logical write to collapse into one
// operation should generate the key once per Terraform operation (e.g. once
// per Create call, reused across that call's own internal retries) rather
// than once per HTTP attempt.
func NewIdempotencyKey(prefix string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("tf-%s-%s", prefix, hex.EncodeToString(buf))
}

// Do issues a request against BaseURL+path, retrying on 429 with backoff
// derived from X-RateLimit-Reset (falling back to exponential backoff if
// the header is absent/unparseable). idempotencyKey is attached to writes
// when non-empty; pass "" for GET/DELETE-without-a-body calls that don't
// need it.
func (c *Client) Do(ctx context.Context, method, path, idempotencyKey string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = b
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := backoffFor(lastErr, attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" && method != http.MethodGet {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response body: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = rateLimitError(resp, respBody)
			continue
		}

		if resp.StatusCode >= 400 {
			return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
		}

		if out != nil {
			data, err := UnwrapEnvelope(respBody)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode response data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("giving up after %d retries: %w", maxRetries, lastErr)
}

type rateLimited struct {
	resetUnix     int64
	retryAfterSec int
	body          string
}

func (r *rateLimited) Error() string { return fmt.Sprintf("rate limited: %s", r.body) }

func rateLimitError(resp *http.Response, body []byte) error {
	rl := &rateLimited{body: string(body)}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			rl.resetUnix = n
		}
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rl.retryAfterSec = n
		}
	}
	return rl
}

// backoffFor computes how long to wait before the given attempt. A 429
// carrying X-RateLimit-Reset (Unix seconds — ApiRateLimitGuard always sets
// it) is honored directly; otherwise falls back to capped exponential
// backoff so a transient network error doesn't retry in a tight loop.
func backoffFor(lastErr error, attempt int) time.Duration {
	if rl, ok := lastErr.(*rateLimited); ok {
		if rl.retryAfterSec > 0 {
			return time.Duration(rl.retryAfterSec) * time.Second
		}
		if rl.resetUnix > 0 {
			d := time.Until(time.Unix(rl.resetUnix, 0))
			if d > 0 && d < maxBackoff {
				return d
			}
			if d >= maxBackoff {
				return maxBackoff
			}
		}
	}
	d := time.Duration(float64(defaultBackoff) * math.Pow(2, float64(attempt-1)))
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}
