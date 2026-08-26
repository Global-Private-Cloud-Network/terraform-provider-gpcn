package client_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

// The message is the only record of the resource, so it must name the ID. The
// cause must also stay reachable for errors.Is.
func TestPartialCreateErrorNamesResourceAndKeepsCause(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("status poll gave up: %w", context.Canceled)
	err := &client.PartialCreateError{ResourceID: "vm-123", Err: cause}

	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is(err, context.Canceled) = false, want the cause to stay reachable")
	}
	if !strings.Contains(err.Error(), "vm-123") {
		t.Errorf("error message %q does not name the resource that the API created", err)
	}
}

// A resource implementation wraps the create error again. The ID must stay in
// the message after that, or the operator cannot find the resource.
func TestPartialCreateErrorSurvivesWrapping(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("creating network: %w", &client.PartialCreateError{ResourceID: "net-9", Err: errors.New("boom")})

	if !strings.Contains(err.Error(), "net-9") {
		t.Errorf("error message %q does not name the resource that the API created", err)
	}

	var partial *client.PartialCreateError
	if !errors.As(err, &partial) {
		t.Fatal("errors.As did not find the PartialCreateError")
	}
	if partial.ResourceID != "net-9" {
		t.Errorf("ResourceID = %q, want %q", partial.ResourceID, "net-9")
	}
}
