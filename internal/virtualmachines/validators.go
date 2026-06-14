package virtualmachines

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ValidateAllNetworksAreNotRemoved(oldNetworksList, newNetworksList types.List) error {
	if oldNetworksList.IsNull() {
		return nil
	}
	// If old networks is not nil and new networks is, that's a problem
	if newNetworksList.IsNull() {
		return errors.New(ErrDetailCannotRemoveLastNetwork)
	}
	return nil
}

// PasswordValidator validates the VM auth password meets complexity requirements.
type PasswordValidator struct{}

var _ validator.String = PasswordValidator{}

func (v PasswordValidator) Description(_ context.Context) string {
	return "Password must be 12-20 characters, contain only letters, digits, and ! @ # % - _ ., and include at least one uppercase letter, one lowercase letter, one digit, and one symbol"
}

func (v PasswordValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v PasswordValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()

	if len(val) < 12 || len(val) > 20 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid password length", "Password must be between 12 and 20 characters")
		return
	}

	allowedChars := regexp.MustCompile(`^[a-zA-Z0-9!@#%\-_.]+$`)
	if !allowedChars.MatchString(val) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid password characters", "Password can only contain letters, digits, and these symbols: ! @ # % - _ .")
		return
	}

	if !regexp.MustCompile(`[A-Z]`).MatchString(val) {
		resp.Diagnostics.AddAttributeError(req.Path, "Password missing uppercase letter", "Password must contain at least one uppercase letter")
	}
	if !regexp.MustCompile(`[a-z]`).MatchString(val) {
		resp.Diagnostics.AddAttributeError(req.Path, "Password missing lowercase letter", "Password must contain at least one lowercase letter")
	}
	if !regexp.MustCompile(`[0-9]`).MatchString(val) {
		resp.Diagnostics.AddAttributeError(req.Path, "Password missing digit", "Password must contain at least one digit")
	}
	if !regexp.MustCompile(`[!@#%\-_.]`).MatchString(val) {
		resp.Diagnostics.AddAttributeError(req.Path, "Password missing symbol", "Password must contain at least one symbol (! @ # % - _ .)")
	}
}

// Determines if the new networks to be added will cause the network interfaces size to exceed its cap
func ValidateNetworkInterfacesDoesNotExceedCap(oldNetworksList, newNetworksList []string, networkInterfaces []networks.ReadVirtualMachineNetworkDataResponseTF) error {
	// Get newly added values
	addedValues, _ := helpers.CheckListForDifferences(oldNetworksList, newNetworksList)
	if len(addedValues)+len(networkInterfaces) > MAX_NETWORKS_ATTACHED_ALLOWED {
		return fmt.Errorf(ErrDetailAddedNetworksExceedsMax, MAX_NETWORKS_ATTACHED_ALLOWED)
	}
	return nil
}
