package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const preflightAuthCheckPath = "/v1/auth/check"

// preflightConfig is the smallest configuration that makes Terraform configure
// the provider: a data source read forces Configure to run first.
func preflightConfig(host string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

data "gpcn_datacenters" "test" {}
`, host)
}

// preflightDatacenterBody is the one row the datacenters read needs. An empty
// list sends the data source down its suggestion path. That path asks for
// regions and countries, and it hides what the test is about.
func preflightDatacenterBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":                  "dc-1",
			"name":                "Kansas",
			"regionId":            1,
			"regionName":          "central",
			"countryId":           "1",
			"countryName":         "United States",
			"countryAbbreviation": "US",
			"gpuEnabled":          true,
			"customImages":        true,
		}},
	}
}

// startPreflightServer serves the datacenters list, and hands every auth-check
// call to authCheck so a test decides what the preflight sees.
func startPreflightServer(t *testing.T, authCheck func(w http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var authCheckCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == preflightAuthCheckPath:
			authCheckCalls.Add(1)
			authCheck(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/":
			testutil.WriteJSONResponse(w, preflightDatacenterBody())
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, &authCheckCalls
}

// TestConfigurePreflightRejectsRevokedKey proves a dead key fails at configure
// time with a diagnostic about the key. Every credential failure answers the
// same opaque 401, so the provider has to name the causes itself.
func TestConfigurePreflightRejectsRevokedKey(t *testing.T) {
	server, authCheckCalls := startPreflightServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"Failed to authenticate.",` +
			`"error":{"code":"Authentication Failed","statusCode":401,"details":null}}`))
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      preflightConfig(server.URL),
			ExpectError: regexp.MustCompile(`API key rejected`),
		}},
	})

	if got := int(authCheckCalls.Load()); got < 1 {
		t.Errorf("auth check calls = %d, want at least 1", got)
	}
}

// TestConfigurePreflightRejectionBytes pins the sentence the 401 answers with.
// The API never says which cause applies, so the list of causes is the whole
// value of the diagnostic.
func TestConfigurePreflightRejectionBytes(t *testing.T) {
	const want = "GPCN answered 401 to GET /v1/auth/check. The key in GPCN_API_KEY was revoked, " +
		"expired, disabled, never bound to an entity, its owner left the entity, the entity is " +
		"deactivated, or the key has exceeded its hourly request limit (1000 per hour). " +
		"Mint a new key in the portal or wait for the limit to reset."

	if ErrDetailAPIKeyRejected != want {
		t.Errorf("ErrDetailAPIKeyRejected =\n%q\nwant\n%q", ErrDetailAPIKeyRejected, want)
	}
}

// TestConfigurePreflightWarnsOnExpiry proves a key that is close to expiry only
// warns. A configure error here would stop an apply that the key can still
// complete.
func TestConfigurePreflightWarnsOnExpiry(t *testing.T) {
	expiresAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)

	server, authCheckCalls := startPreflightServer(t, func(w http.ResponseWriter) {
		testutil.WriteJSONResponse(w, map[string]any{
			"success": true,
			"message": "",
			"data": map[string]any{
				"authenticated": true,
				"credential": map[string]any{
					"kind":      "api_key",
					"id":        "key-1",
					"name":      "terraform",
					"keyStart":  "gpcn_abcd",
					"entityId":  "entity-1",
					"expiresAt": expiresAt,
				},
				"grants": map[string]any{
					"source":      "api_key_role",
					"entityId":    "entity-1",
					"permissions": []string{"vpc:read"},
				},
			},
			"meta": nil,
		})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: preflightConfig(server.URL),
			Check: resource.TestCheckResourceAttr(
				"data.gpcn_datacenters.test", "datacenters.0.name", "Kansas"),
		}},
	})

	if got := int(authCheckCalls.Load()); got < 1 {
		t.Errorf("auth check calls = %d, want at least 1", got)
	}
}

// TestConfigurePreflightWarningBytes pins the warning sentences. The test
// harness runs a step to completion or not at all. It cannot read a warning
// back. This test calls the reporter and reads the diagnostics it builds.
func TestConfigurePreflightWarningBytes(t *testing.T) {
	stringPtr := func(value string) *string { return &value }

	soon := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name        string
		credential  *client.AuthCheckCredential
		wantSummary string
		wantDetail  string
	}{
		{
			name:       "no credential",
			credential: nil,
		},
		{
			name:       "an api key with no expiry",
			credential: &client.AuthCheckCredential{Kind: "api_key", KeyStart: stringPtr("gpcn_test")},
		},
		{
			name:       "an api key that expires past the window",
			credential: &client.AuthCheckCredential{Kind: "api_key", ExpiresAt: &later},
		},
		{
			name:        "an api key that expires inside the window",
			credential:  &client.AuthCheckCredential{Kind: "api_key", ExpiresAt: &soon},
			wantSummary: "API key expires soon",
			wantDetail:  "The API key expires at " + soon + ".",
		},
		{
			name:        "a credential of another kind",
			credential:  &client.AuthCheckCredential{Kind: "session"},
			wantSummary: "The configured credential is not an API key",
			wantDetail:  `GPCN reports credential kind "session".`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var diags diag.Diagnostics
			reportPreflight(t.Context(), &client.AuthCheckData{Credential: tt.credential}, &diags)

			if diags.HasError() {
				t.Fatalf("preflight added an error diagnostic: %v", diags.Errors())
			}
			if tt.wantSummary == "" {
				if len(diags) != 0 {
					t.Fatalf("diagnostics = %v, want none", diags)
				}
				return
			}
			if len(diags) != 1 {
				t.Fatalf("diagnostics = %v, want exactly one warning", diags)
			}
			if got := diags[0].Summary(); got != tt.wantSummary {
				t.Errorf("summary = %q, want %q", got, tt.wantSummary)
			}
			if got := diags[0].Detail(); got != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got, tt.wantDetail)
			}
		})
	}
}

// TestConfigurePreflightNamesHostOnRouteNotFound proves a wrong host is named as
// such. A host that points at something else answers the bare route-not-found
// body, never a 401. A key diagnostic would then send the user to the wrong
// setting.
func TestConfigurePreflightNamesHostOnRouteNotFound(t *testing.T) {
	server, _ := startPreflightServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Route Not Found"}`))
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      preflightConfig(server.URL),
			ExpectError: regexp.MustCompile(`Check GPCN_HOST`),
		}},
	})
}
