package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// AuthCredentialKindAPIKey is the credential kind the API reports for a key.
// The other kind is a browser session, which a provider never holds.
const AuthCredentialKindAPIKey = "api_key"

// AuthCheckCredential describes the credential that authenticated the call.
// Every nullable field is a pointer. The API sends null for a value it does not
// have, and an empty string is a value.
type AuthCheckCredential struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	KeyStart  *string `json:"keyStart"`
	EntityID  *string `json:"entityId"`
	ExpiresAt *string `json:"expiresAt"`
}

// AuthCheckGrants describes what the credential is allowed to do.
type AuthCheckGrants struct {
	Source      string   `json:"source"`
	EntityID    *string  `json:"entityId"`
	Permissions []string `json:"permissions"`
}

// AuthCheckData is the payload of a successful auth check.
type AuthCheckData struct {
	Authenticated bool                 `json:"authenticated"`
	Credential    *AuthCheckCredential `json:"credential"`
	Grants        AuthCheckGrants      `json:"grants"`
}

type authCheckResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    AuthCheckData `json:"data"`
}

// AuthCheck asks the API who the configured key is, and stashes the tenant and
// the permission ceiling it reports. It is the cheapest authenticated call, and
// it needs no permission. It is the only call that reports the key's kind and
// lifetime.
func (c *GpcnClient) AuthCheck(ctx context.Context) (*AuthCheckData, error) {
	ctx = WithCorrelationID(ctx)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, AUTH_CHECK_URL_V1, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth check request: %w", err)
	}

	response, err := c.DoWithRetry(request)
	if err != nil {
		// Returned unwrapped, so the caller can read the status off it.
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			tflog.Warn(ctx, "failed to close response body", map[string]any{"error": closeErr.Error()})
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth check response: %w", err)
	}

	var decoded authCheckResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse auth check response: %w", err)
	}

	c.entityID = derefString(decoded.Data.Grants.EntityID)
	c.permissions = slices.Clone(decoded.Data.Grants.Permissions)

	return &decoded.Data, nil
}

// EntityID returns the tenant every call with this key acts in, as the API
// reported it at configure time. It is empty until AuthCheck has run.
func (c *GpcnClient) EntityID() string {
	return c.entityID
}

// Permissions returns the permission ceiling the API reported for this key.
// A permission that is absent is a reliable refusal, but one that is present
// is not a promise: a route can narrow the grant further.
func (c *GpcnClient) Permissions() []string {
	return slices.Clone(c.permissions)
}
