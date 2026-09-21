package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"

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
// list sends the data source down its suggestion path, which asks for regions
// and countries and hides what the test is about.
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

// TestConfigurePreflightNamesHostOnRouteNotFound proves a wrong host is named as
// such. A host that points at something else answers the bare route-not-found
// body, never a 401, so a key diagnostic would send the user hunting the wrong
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
