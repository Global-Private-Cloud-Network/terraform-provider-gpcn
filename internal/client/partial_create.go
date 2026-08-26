package client

import "fmt"

// PartialCreateError shows that the GPCN API accepted a create request and gave
// a resource ID, but the provider did not complete the create. Usually the user
// interrupted the run.
//
// The resource exists and the account pays for it, and Terraform has no record
// of it. The message is the only way the operator learns the ID, so it must
// name the resource and say what to do next.
type PartialCreateError struct {
	// ResourceID is the ID that the API gave to the new resource.
	ResourceID string
	// Err is the error that stopped the create.
	Err error
}

func (e *PartialCreateError) Error() string {
	return fmt.Sprintf("the API created resource %s, but the provider did not confirm that it is ready. "+
		"Terraform does not have the resource in the state. Import it or remove it manually. Cause: %v",
		e.ResourceID, e.Err)
}

func (e *PartialCreateError) Unwrap() error { return e.Err }
