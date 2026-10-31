// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// This file is the "resource test harness against a mocked HTTP server"
// called for in ROADMAP-V70 E31: no live CMDash API is used anywhere here
// — mockCmdashServer below is a self-contained in-memory fake of
// /v2/webhooks (apps/api/src/public-api/v2/public-webhooks-v2.controller.ts's
// GET/POST/PUT/DELETE), and every TestStep's implicit post-apply
// `terraform plan` (terraform-plugin-testing's standard behavior — a
// nonempty plan there fails the test unless ExpectNonEmptyPlan is set) is
// exactly the "plan must be empty after apply" requirement.
//
// Gated by TF_ACC (resource.Test's own contract: it calls t.Skip() when
// TF_ACC != "1"), so `go test ./...` in CI does NOT require a real
// terraform binary or network access — only `TF_ACC=1 go test ./...` runs
// this, matching how every other terraform-plugin-framework provider in
// the ecosystem gates its acceptance suite.

func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"cmdash": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// writeEnvelope answers the way the real API does: every success is wrapped in
// the ResponseInterceptor envelope {"success":true,"requestId":...,
// "timestamp":...,"data":<payload>} (ROADMAP-A1009 P1, audit 2026-10-09 API-1).
// This mock used to encode bare payloads, which is the only reason the provider
// passed its own tests while being unable to decode a real response. The shape
// is pinned by packages/shared/src/envelope/envelope-contract.fixture.json.
func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"requestId": "req-contract-fixture-0001",
		"timestamp": "2026-10-09T12:00:00.000Z",
		"data":      data,
	})
}

// mockCmdashServer is a minimal, tenant-unaware fake of /v2/webhooks with
// in-memory state, enough to drive Create/Read/Update/Delete/Import
// end to end. Field shapes match WebhookListItemDto/WebhookCreateResultDto
// exactly (apps/api/src/public-api/dto/openapi-schemas.ts).
func mockCmdashServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	type webhook struct {
		ID        string   `json:"id"`
		URL       string   `json:"url"`
		Events    []string `json:"events"`
		IsActive  bool     `json:"isActive"`
		CreatedAt string   `json:"createdAt"`
	}
	store := map[string]*webhook{}
	nextID := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/webhooks", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			list := make([]*webhook, 0, len(store))
			for _, wh := range store {
				list = append(list, wh)
			}
			writeEnvelope(w, list)
		case http.MethodPost:
			var body struct {
				URL    string   `json:"url"`
				Events []string `json:"events"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			nextID++
			id := fmt.Sprintf("wh-%d", nextID)
			wh := &webhook{ID: id, URL: body.URL, Events: body.Events, IsActive: true, CreatedAt: "2026-09-04T00:00:00.000Z"}
			store[id] = wh
			w.WriteHeader(http.StatusCreated)
			writeEnvelope(w, struct {
				*webhook
				Secret string `json:"secret"`
			}{wh, "whsec_mocktestsecret000000000000"})
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/v2/webhooks/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		id := r.URL.Path[len("/api/v2/webhooks/"):]
		wh, ok := store[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "not found"})
			return
		}
		switch r.Method {
		case http.MethodPut:
			var body struct {
				URL      *string   `json:"url"`
				Events   *[]string `json:"events"`
				IsActive *bool     `json:"isActive"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.URL != nil {
				wh.URL = *body.URL
			}
			if body.Events != nil {
				wh.Events = *body.Events
			}
			if body.IsActive != nil {
				wh.IsActive = *body.IsActive
			}
			writeEnvelope(w, wh)
		case http.MethodDelete:
			delete(store, id)
			w.WriteHeader(http.StatusOK)
			writeEnvelope(w, map[string]any{"deleted": true})
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAccWebhookRegistrationResource_PlanEmptyAfterApply(t *testing.T) {
	srv := mockCmdashServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "cmdash" {
  api_key  = "cmdash_ek_test"
  base_url = %q
}

resource "cmdash_webhook_registration" "test" {
  url    = "https://example.com/hooks/cmdash"
  events = ["posture.changed", "risk.created"]
}
`, srv.URL+"/api"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "url", "https://example.com/hooks/cmdash"),
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "events.#", "2"),
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "is_active", "true"),
					resource.TestCheckResourceAttrSet("cmdash_webhook_registration.test", "id"),
					resource.TestCheckResourceAttrSet("cmdash_webhook_registration.test", "secret"),
				),
				// No ExpectNonEmptyPlan — the framework runs `terraform plan`
				// after this apply and fails the test if it's non-empty. This
				// IS the "plan must be empty after apply" check.
			},
			{
				// Import support: re-attach to the same object by ID and
				// verify state comes back identical (secret excluded — the
				// API never returns it again, see the schema description).
				ResourceName:            "cmdash_webhook_registration.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
				Config: fmt.Sprintf(`
provider "cmdash" {
  api_key  = "cmdash_ek_test"
  base_url = %q
}

resource "cmdash_webhook_registration" "test" {
  url    = "https://example.com/hooks/cmdash"
  events = ["posture.changed", "risk.created"]
}
`, srv.URL+"/api"),
			},
		},
	})
}

func TestAccWebhookRegistrationResource_UpdateInPlace(t *testing.T) {
	srv := mockCmdashServer(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "cmdash" {
  api_key  = "cmdash_ek_test"
  base_url = %q
}

resource "cmdash_webhook_registration" "test" {
  url    = "https://example.com/hooks/a"
  events = ["posture.changed"]
}
`, srv.URL+"/api"),
			},
			{
				Config: fmt.Sprintf(`
provider "cmdash" {
  api_key  = "cmdash_ek_test"
  base_url = %q
}

resource "cmdash_webhook_registration" "test" {
  url       = "https://example.com/hooks/b"
  events    = ["posture.changed", "risk.created"]
  is_active = false
}
`, srv.URL+"/api"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "url", "https://example.com/hooks/b"),
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "events.#", "2"),
					resource.TestCheckResourceAttr("cmdash_webhook_registration.test", "is_active", "false"),
				),
			},
		},
	})
}
