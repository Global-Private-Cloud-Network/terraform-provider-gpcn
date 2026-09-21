package client_test

import (
	"errors"
	"fmt"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

// TestIsForbiddenIsConflictIsUnauthorized pins the status predicates and the
// code accessors. Each must unwrap, because a CRUD action wraps the transport
// error before a resource inspects it.
func TestIsForbiddenIsConflictIsUnauthorized(t *testing.T) {
	forbidden := &client.HTTPError{StatusCode: 403, Code: "LEGACY_NETWORKING_CUT_OVER", Message: "moved"}
	conflict := &client.HTTPError{StatusCode: 409, Code: "VPC_NOT_EMPTY", Message: "not empty"}
	unauthorized := &client.HTTPError{StatusCode: 401, Code: "Authentication Failed", Message: "Failed to authenticate."}

	statusTests := []struct {
		name                                        string
		err                                         error
		wantForbidden, wantConflict, wantUnauthoriz bool
	}{
		{"nil error", nil, false, false, false},
		{"403", forbidden, true, false, false},
		{"409", conflict, false, true, false},
		{"401", unauthorized, false, false, true},
		{"404", &client.HTTPError{StatusCode: 404}, false, false, false},
		{"500", &client.HTTPError{StatusCode: 500}, false, false, false},
		{"wrapped 403", fmt.Errorf("create vpc: %w", forbidden), true, false, false},
		{"wrapped 409", fmt.Errorf("delete vpc: %w", conflict), false, true, false},
		{"wrapped 401", fmt.Errorf("read: %w", unauthorized), false, false, true},
		{"non-HTTP error", errors.New("connection refused"), false, false, false},
	}

	for _, tt := range statusTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := client.IsForbidden(tt.err); got != tt.wantForbidden {
				t.Errorf("IsForbidden(%v) = %v, want %v", tt.err, got, tt.wantForbidden)
			}
			if got := client.IsConflict(tt.err); got != tt.wantConflict {
				t.Errorf("IsConflict(%v) = %v, want %v", tt.err, got, tt.wantConflict)
			}
			if got := client.IsUnauthorized(tt.err); got != tt.wantUnauthoriz {
				t.Errorf("IsUnauthorized(%v) = %v, want %v", tt.err, got, tt.wantUnauthoriz)
			}
		})
	}

	codeTests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"code present", conflict, "VPC_NOT_EMPTY"},
		{"wrapped code", fmt.Errorf("delete vpc: %w", conflict), "VPC_NOT_EMPTY"},
		{"unparseable body has no code", &client.HTTPError{StatusCode: 502, Body: "<html>"}, ""},
		{"non-HTTP error", errors.New("connection refused"), ""},
	}

	for _, tt := range codeTests {
		t.Run("ErrorCode/"+tt.name, func(t *testing.T) {
			if got := client.ErrorCode(tt.err); got != tt.want {
				t.Errorf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
			if got := client.HasErrorCode(tt.err, tt.want); tt.want != "" && !got {
				t.Errorf("HasErrorCode(%v, %q) = false, want true", tt.err, tt.want)
			}
			if client.HasErrorCode(tt.err, "NO_SUCH_CODE") {
				t.Errorf("HasErrorCode(%v, \"NO_SUCH_CODE\") = true, want false", tt.err)
			}
		})
	}

	// An empty code must never match, or a transport failure would answer yes
	// to every code a resource asks about.
	if client.HasErrorCode(errors.New("connection refused"), "") {
		t.Error("HasErrorCode(non-HTTP error, \"\") = true, want false")
	}
}
