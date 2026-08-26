package client

import (
	"errors"
	"fmt"
)

// PartialCreateError shows that the GPCN API accepted a create request and gave
// a resource ID, but the provider did not complete the create. Usually the user
// interrupted the run.
//
// The resource exists and the account pays for it. A resource implementation
// must write ResourceID to the Terraform state before it reports the error. If
// it does not, no later plan, refresh, or destroy can find the resource.
type PartialCreateError struct {
	// ResourceID is the ID that the API gave to the new resource.
	ResourceID string
	// Err is the error that stopped the create.
	Err error
}

func (e *PartialCreateError) Error() string {
	return fmt.Sprintf("resource %s was created but the provider could not confirm it was ready: %v", e.ResourceID, e.Err)
}

func (e *PartialCreateError) Unwrap() error { return e.Err }

// NewPartialCreateError wraps err in a PartialCreateError for resourceID. If err
// is nil, this function returns nil. Therefore you can call it directly in an
// error return statement.
func NewPartialCreateError(resourceID string, err error) error {
	if err == nil {
		return nil
	}
	return &PartialCreateError{ResourceID: resourceID, Err: err}
}

// PartialCreateResourceID returns the ID of the resource that the API created.
// The second result is false if err has no PartialCreateError.
func PartialCreateResourceID(err error) (string, bool) {
	var partial *PartialCreateError
	if errors.As(err, &partial) && partial.ResourceID != "" {
		return partial.ResourceID, true
	}
	return "", false
}
