package client_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/client"
)

func TestPartialCreateErrorPreservesCauseAndID(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("status poll gave up: %w", context.Canceled)
	err := client.NewPartialCreateError("vm-123", cause)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, want the cause to stay reachable")
	}
	if !strings.Contains(err.Error(), "vm-123") {
		t.Errorf("error message %q does not name the resource that was created", err)
	}

	id, ok := client.PartialCreateResourceID(err)
	if !ok || id != "vm-123" {
		t.Errorf("PartialCreateResourceID = (%q, %v), want (\"vm-123\", true)", id, ok)
	}
}

// A resource implementation wraps the create error again. The ID must stay
// available after that, or the resource is still lost.
func TestPartialCreateResourceIDFoundThroughWrapping(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("creating network: %w", client.NewPartialCreateError("net-9", errors.New("boom")))

	id, ok := client.PartialCreateResourceID(err)
	if !ok || id != "net-9" {
		t.Errorf("PartialCreateResourceID = (%q, %v), want (\"net-9\", true)", id, ok)
	}
}

func TestPartialCreateResourceIDIgnoresOtherErrors(t *testing.T) {
	t.Parallel()

	if id, ok := client.PartialCreateResourceID(errors.New("plain failure")); ok {
		t.Errorf("PartialCreateResourceID = (%q, true), want (\"\", false) for an unrelated error", id)
	}
	if id, ok := client.PartialCreateResourceID(nil); ok {
		t.Errorf("PartialCreateResourceID = (%q, true), want (\"\", false) for a nil error", id)
	}
	if err := client.NewPartialCreateError("vm-1", nil); err != nil {
		t.Errorf("NewPartialCreateError with a nil cause = %v, want nil", err)
	}
}
