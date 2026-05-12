package virtualmachines

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Validates that the planned virtual machine size is larger than the current. Returns the new sizeId if so
func ValidatePlanSizeLargerThanStateSize(gpcnClient *client.GpcnClient, ctx context.Context, state, plan ResourceModel) (string, error) {
	tflog.Info(ctx, LogSizeChangedVerifyingLarger)
	var planSize ResourceModelSize
	plan.Size.As(ctx, &planSize, basetypes.ObjectAsOptions{})
	var stateSize ResourceModelSize
	state.Size.As(ctx, &stateSize, basetypes.ObjectAsOptions{})
	// This had preliminary validation, but verify it's up-to-date
	_, sizes, err := GetVirtualMachineSizeSkuId(gpcnClient, ctx, plan.DatacenterId.ValueString(), planSize.Name.ValueString())
	if err != nil {
		return "", err
	}

	// Verify the new size is larger than the old
	stateSizeIdx := slices.IndexFunc(sizes, func(virtualMachineSize VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(virtualMachineSize.Name.ValueString(), stateSize.Name.ValueString())
	})
	planSizeIdx := slices.IndexFunc(sizes, func(virtualMachineSize VirtualMachineConfigurationsTF) bool {
		return strings.EqualFold(virtualMachineSize.Name.ValueString(), planSize.Name.ValueString())
	})

	if sizes[stateSizeIdx].CPUCores.ValueInt64() > sizes[planSizeIdx].CPUCores.ValueInt64() {
		return "", errors.New("the target virtual machine size is smaller than the old size. This shouldn't happen, since the validation should check for a smaller size (and force a replace), but in case it does, make sure the target size is LARGER than the current")
	}

	return sizes[planSizeIdx].SkuId.ValueString(), nil
}

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

// ImageNameValidator warns (not errors) when image_name is not in ValidImageNames.
// This allows custom or future images while surfacing a hint about known-good values.
type ImageNameValidator struct{}

var _ validator.String = ImageNameValidator{}

func (v ImageNameValidator) Description(_ context.Context) string {
	return fmt.Sprintf("Image name should be one of the supported images: %s. Since upstream images may change before this provider is updated, and not every image is available for every datacenter, entering an invalid image is non-blocking, but it may not be available.", strings.Join(ValidImageNames, ", "))
}

func (v ImageNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ImageNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if slices.Contains(ValidImageNames, val) {
		return
	}
	resp.Diagnostics.AddAttributeWarning(
		req.Path,
		"Unrecognized image name",
		fmt.Sprintf("%q is not a known supported image. There is no guarantee that the image specified is available in GPCN. Known images: %s", val, strings.Join(ValidImageNames, ", ")),
	)
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
