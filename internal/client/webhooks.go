// SPDX-License-Identifier: Apache-2.0

package client

import "context"

// Webhook mirrors WebhookListItemDto (apps/api/src/public-api/dto/openapi-schemas.ts).
type Webhook struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	IsActive  bool     `json:"isActive"`
	CreatedAt string   `json:"createdAt"`
}

// WebhookWithSecret mirrors WebhookCreateResultDto — the ONLY response that
// ever carries the plaintext HMAC secret at creation time.
type WebhookWithSecret struct {
	Webhook
	Secret string `json:"secret"`
}

// WebhookRotateResult mirrors WebhookRotateSecretResultDto.
type WebhookRotateResult struct {
	ID                      string `json:"id"`
	Secret                  string `json:"secret"`
	SecretRotatedAt         string `json:"secretRotatedAt"`
	PreviousSecretExpiresAt string `json:"previousSecretExpiresAt"`
	OverlapHours            int    `json:"overlapHours"`
}

type createWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

type updateWebhookRequest struct {
	URL      *string   `json:"url,omitempty"`
	Events   *[]string `json:"events,omitempty"`
	IsActive *bool     `json:"isActive,omitempty"`
}

// ListWebhooks — GET /v2/webhooks. Requires the `webhooks:read` key scope.
// Unpaginated: WebhookService.list returns every registration for the
// tenant (there is no per-tenant cap in the controller — verified against
// public-webhooks-v2.controller.ts `list()`), which is fine in practice
// because webhook registrations are a low-cardinality admin resource, not
// a data feed.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if err := c.Do(ctx, "GET", "/v2/webhooks", "", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWebhook — POST /v2/webhooks. Requires `webhooks:write`. idempotencyKey
// should be stable across retries of the SAME Terraform apply of this
// resource instance and unique across different instances.
func (c *Client) CreateWebhook(ctx context.Context, idempotencyKey, url string, events []string) (*WebhookWithSecret, error) {
	var out WebhookWithSecret
	body := createWebhookRequest{URL: url, Events: events}
	if err := c.Do(ctx, "POST", "/v2/webhooks", idempotencyKey, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWebhook — PUT /v2/webhooks/:id. Requires `webhooks:write`. Any of
// url/events/isActive may be nil to leave that field unchanged (the API
// treats absent fields as "no change" — public-webhooks-v2.controller.ts
// `update()` passes the parsed, partial body straight to WebhookService.update).
func (c *Client) UpdateWebhook(ctx context.Context, idempotencyKey, id string, url *string, events *[]string, isActive *bool) (*Webhook, error) {
	var out Webhook
	body := updateWebhookRequest{URL: url, Events: events, IsActive: isActive}
	if err := c.Do(ctx, "PUT", "/v2/webhooks/"+id, idempotencyKey, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook — DELETE /v2/webhooks/:id. Requires `webhooks:write`.
func (c *Client) DeleteWebhook(ctx context.Context, idempotencyKey, id string) error {
	return c.Do(ctx, "DELETE", "/v2/webhooks/"+id, idempotencyKey, nil, nil)
}

// GetWebhook fetches one webhook by listing and filtering client-side —
// there is no GET /v2/webhooks/:id route (only :id/logs and :id/rotate-
// secret take an id; list() is the only way to read a single registration's
// current url/events/isActive). Verified against
// public-webhooks-v2.controller.ts: it has no single-item GET. This is a
// v2 API gap for a Terraform provider (see docs/terraform-provider.md) —
// Read/Import here cost one extra round trip (an N-item list) instead of
// one direct GET.
func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	all, err := c.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range all {
		if w.ID == id {
			return &w, nil
		}
	}
	return nil, &APIError{StatusCode: 404, Body: `{"message":"webhook not found in list"}`}
}
