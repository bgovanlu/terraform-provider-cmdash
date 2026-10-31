// SPDX-License-Identifier: Apache-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ROADMAP-A1009 P1 (audit 2026-10-09 API-1): the provider decoded the raw
// response body, but the API wraps every success in
// {"success":true,"requestId":...,"timestamp":...,"data":<payload>}. These
// tests serve the wire bodies the REAL ResponseInterceptor emits
// (testdata/envelope-contract.fixture.json, a copy of
// packages/shared/src/envelope/envelope-contract.fixture.json, proven against
// the interceptor by apps/api/test/client-envelope-contract-a1009-p1.spec.ts)
// and drive the real Client methods against them.

// The first registration in the shared fixture's webhooks.* cases.
const fixtureWebhookID = "0e1f6b46-3a5d-4c3b-9d7a-0c8f6d7e2a01"

const sharedFixturePath = "../../../packages/shared/src/envelope/envelope-contract.fixture.json"

type fixtureCase struct {
	Name    string `json:"name"`
	Request struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"request"`
	Wire json.RawMessage `json:"wire"`
	Data json.RawMessage `json:"data"`
}

type fixtureFile struct {
	Cases []fixtureCase `json:"cases"`
}

func loadFixture(t *testing.T) fixtureFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/envelope-contract.fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return f
}

func (f fixtureFile) wire(t *testing.T, name string) []byte {
	t.Helper()
	for _, c := range f.Cases {
		if c.Name == name {
			return c.Wire
		}
	}
	t.Fatalf("no fixture case %q", name)
	return nil
}

// serveWire answers every request with the given wire body.
func serveWire(t *testing.T, status int, body []byte) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "cmdash_ek_test")
}

// The vendored copy must not drift from the shared fixture. In a standalone
// checkout of this module (the registry mirror) the shared file does not exist
// and the check is skipped.
func TestFixtureCopyMatchesSharedFixture(t *testing.T) {
	shared, err := os.ReadFile(sharedFixturePath)
	if err != nil {
		t.Skipf("shared fixture not present (standalone checkout): %v", err)
	}
	local, err := os.ReadFile("testdata/envelope-contract.fixture.json")
	if err != nil {
		t.Fatalf("read local fixture: %v", err)
	}
	if !bytes.Equal(shared, local) {
		t.Fatal("testdata/envelope-contract.fixture.json differs from packages/shared/src/envelope/envelope-contract.fixture.json; copy the shared file over it")
	}
}

func TestUnwrapEnvelope_EveryFixtureCase(t *testing.T) {
	f := loadFixture(t)
	if len(f.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range f.Cases {
		if len(c.Data) == 0 {
			continue // void handler: no data key, covered by the failure test below
		}
		got, err := UnwrapEnvelope(c.Wire)
		if err != nil {
			t.Fatalf("%s: UnwrapEnvelope: %v", c.Name, err)
		}
		var a, b any
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatalf("%s: unmarshal got: %v", c.Name, err)
		}
		if err := json.Unmarshal(c.Data, &b); err != nil {
			t.Fatalf("%s: unmarshal want: %v", c.Name, err)
		}
		ga, _ := json.Marshal(a)
		gb, _ := json.Marshal(b)
		if string(ga) != string(gb) {
			t.Fatalf("%s: unwrapped %s, want %s", c.Name, ga, gb)
		}
	}
}

func TestUnwrapEnvelope_RefusesBodiesThatAreNotTheEnvelope(t *testing.T) {
	f := loadFixture(t)
	cases := map[string][]byte{
		"bare array":        []byte(`[{"id":"wh-1"}]`),
		"bare object":       []byte(`{"id":"wh-1"}`),
		"object with data":  []byte(`{"data":[{"id":"wh-1"}]}`),
		"success false":     []byte(`{"success":false,"requestId":"r","timestamp":"t","data":{}}`),
		"envelope, no data": f.wire(t, "void handler"),
		"not json":          []byte(`<html>`),
		"missing requestId": []byte(`{"success":true,"timestamp":"t","data":{}}`),
		"empty body":        []byte(``),
	}
	for name, body := range cases {
		if _, err := UnwrapEnvelope(body); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

func TestListWebhooks_DecodesTheRealEnvelope(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 200, f.wire(t, "webhooks.list"))
	got, err := c.ListWebhooks(context.Background())
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 webhooks, got %d", len(got))
	}
	if got[0].ID != fixtureWebhookID || got[0].URL == "" || len(got[0].Events) != 1 || !got[0].IsActive {
		t.Fatalf("webhook 0 not decoded: %+v", got[0])
	}
}

func TestGetWebhook_FindsTheRegistrationInTheEnvelopedList(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 200, f.wire(t, "webhooks.list"))
	w, err := c.GetWebhook(context.Background(), "9a2c1d4e-7b6f-4e0a-8c15-3d4b5a6f7e02")
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if len(w.Events) != 2 {
		t.Fatalf("expected 2 events, got %+v", w)
	}
	if _, err := c.GetWebhook(context.Background(), "missing"); !IsNotFound(err) {
		t.Fatalf("expected IsNotFound for an unknown id, got %v", err)
	}
}

func TestCreateWebhook_ReturnsTheIssuedIDAndSecret(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 201, f.wire(t, "webhooks.create"))
	got, err := c.CreateWebhook(context.Background(), "tf-key-12345678", "https://hooks.zapier.com/hooks/standard/1/abc", []string{"risk.created"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	// The old code decoded the wrapper into this struct: ID and Secret came
	// back empty and the resource was stored in state with no id.
	if got.ID != fixtureWebhookID {
		t.Fatalf("ID = %q", got.ID)
	}
	if !strings.HasPrefix(got.Secret, "whsec_") {
		t.Fatalf("Secret = %q", got.Secret)
	}
}

func TestUpdateWebhook_DecodesTheRealEnvelope(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 200, f.wire(t, "webhooks.update"))
	events := []string{"risk.created", "risk.updated"}
	got, err := c.UpdateWebhook(context.Background(), "tf-key-12345678", fixtureWebhookID, nil, &events, nil)
	if err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	if got.ID == "" || len(got.Events) != 2 {
		t.Fatalf("not decoded: %+v", got)
	}
}

func TestDeleteWebhook_AcceptsTheRealEnvelope(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 200, f.wire(t, "webhooks.delete"))
	if err := c.DeleteWebhook(context.Background(), "tf-key-12345678", fixtureWebhookID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
}

func TestReadOnlyCalls_DecodeTheRealEnvelope(t *testing.T) {
	f := loadFixture(t)

	frameworks, err := serveWire(t, 200, f.wire(t, "compliance.frameworks.list")).ListFrameworks(context.Background())
	if err != nil || len(frameworks) != 1 || frameworks[0].Slug != "nist-csf" || frameworks[0].TotalControls != 106 {
		t.Fatalf("ListFrameworks: %+v, %v", frameworks, err)
	}

	controls, err := serveWire(t, 200, f.wire(t, "compliance.framework.controls")).GetFrameworkControls(context.Background(), "nist-csf")
	if err != nil || len(controls) != 1 || controls[0].ControlID != "PR.AC-1" || controls[0].Status != "IMPLEMENTED" {
		t.Fatalf("GetFrameworkControls: %+v, %v", controls, err)
	}

	conns, err := serveWire(t, 200, f.wire(t, "integrations.list")).ListIntegrationConnections(context.Background())
	if err != nil || len(conns) != 1 || conns[0].Provider != "GITHUB" || conns[0].LastSyncAt == nil {
		t.Fatalf("ListIntegrationConnections: %+v, %v", conns, err)
	}
}

// The old behaviour: a bare array decoded straight into the payload type. It
// must now be an error, not a quietly-correct-looking result.
func TestListWebhooks_FailsLoudlyOnAnUnenvelopedBody(t *testing.T) {
	c := serveWire(t, 200, []byte(`[{"id":"wh-1","url":"https://x","events":["risk.created"],"isActive":true,"createdAt":"2026-10-01T00:00:00.000Z"}]`))
	if got, err := c.ListWebhooks(context.Background()); err == nil {
		t.Fatalf("expected an error for a body that is not the envelope, got %+v", got)
	}
}

func TestCreateWebhook_FailsLoudlyWhenTheEnvelopeHasNoData(t *testing.T) {
	f := loadFixture(t)
	c := serveWire(t, 201, f.wire(t, "void handler"))
	if got, err := c.CreateWebhook(context.Background(), "tf-key-12345678", "https://x", []string{"risk.created"}); err == nil {
		t.Fatalf("expected an error, got %+v", got)
	}
}
