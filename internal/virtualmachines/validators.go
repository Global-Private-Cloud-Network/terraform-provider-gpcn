package virtualmachines

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// PublicIpIdConflictsValidator refuses a held address beside a request for a new one.
type PublicIpIdConflictsValidator struct{}

var _ validator.String = PublicIpIdConflictsValidator{}

func (v PublicIpIdConflictsValidator) Description(_ context.Context) string {
	return "public_ip_id must not be set when allocate_public_ip is true"
}

func (v PublicIpIdConflictsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// GPCN refuses the two together, because one create cannot both claim a held address and
// mint a new one. The provider says so at plan time instead of paying for a round trip.
func (v PublicIpIdConflictsValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var allocatePublicIp types.Bool
	diags := req.Config.GetAttribute(ctx, path.Root("allocate_public_ip"), &allocatePublicIp)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if allocatePublicIp.ValueBool() {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryPublicIpConflict, ErrDetailPublicIpIdConflictsWithAllocate)
	}
}

// stringvalidator.LengthAtMost counts characters. GPCN™ caps the length in bytes of UTF-8.
type MaxBytesValidator struct {
	Max int
}

var _ validator.String = MaxBytesValidator{}

func (v MaxBytesValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be at most %d bytes of UTF-8", v.Max)
}

func (v MaxBytesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v MaxBytesValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	if len(value) > v.Max {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryTextTooLong, fmt.Sprintf(ErrDetailTextTooLong, v.Max, len(value)))
	}
}

type BasicPlaneValidator struct{}

var _ validator.String = BasicPlaneValidator{}

func (v BasicPlaneValidator) Description(_ context.Context) string {
	return "value must not contain a character above U+FFFF"
}

func (v BasicPlaneValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v BasicPlaneValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	for _, character := range req.ConfigValue.ValueString() {
		if character > 0xFFFF {
			resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryUserDataCharacter, ErrDetailUserDataCharacter)
			return
		}
	}
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
