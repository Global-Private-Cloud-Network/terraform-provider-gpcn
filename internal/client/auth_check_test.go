package client_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

// authCheckBody gives grants an entity id the credential does not carry. The
// two agree in production. They differ here so a stash bound to the wrong
// field fails the test.
const authCheckBody = `{
  "success": true,
  "message": "",
  "data": {
    "authenticated": true,
    "credential": {
      "kind": "api_key",
      "id": "key-1",
      "name": "terraform",
      "keyStart": "gpcn_abc",
      "entityId": "entity-on-the-credential",
      "expiresAt": null
    },
    "grants": {
      "source": "api_key_role",
      "entityId": "entity-on-the-grants",
      "permissions": ["virtual-machine:read", "virtual-machine:create"]
    }
  },
  "meta": null
}`

func TestAuthCheckStashesGrants(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/check" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authCheckBody))
	}))
	defer server.Close()

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 0
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	data, err := gpcnClient.AuthCheck(t.Context())
	if err != nil {
		t.Fatalf("AuthCheck returned unexpected error: %v", err)
	}

	var decoded struct {
		Data struct {
			Grants struct {
				EntityID    string   `json:"entityId"`
				Permissions []string `json:"permissions"`
			} `json:"grants"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(authCheckBody), &decoded); err != nil {
		t.Fatalf("failed to parse the fixture: %v", err)
	}
	wantEntityID := decoded.Data.Grants.EntityID
	wantPermissions := decoded.Data.Grants.Permissions

	if data.Grants.EntityID == nil || *data.Grants.EntityID != wantEntityID {
		t.Errorf("AuthCheck returned grants entity %v, want %q", data.Grants.EntityID, wantEntityID)
	}

	if got := gpcnClient.EntityID(); got != wantEntityID {
		t.Errorf("EntityID() = %q, want %q", got, wantEntityID)
	}

	got := gpcnClient.Permissions()
	if !slices.Equal(got, wantPermissions) {
		t.Errorf("Permissions() = %v, want %v", got, wantPermissions)
	}

	// A caller that edits the returned slice must not reach the stash.
	if len(got) > 0 {
		got[0] = "tampered"
	}
	if again := gpcnClient.Permissions(); !slices.Equal(again, wantPermissions) {
		t.Errorf("Permissions() after a caller edited the result = %v, want %v", again, wantPermissions)
	}
}
