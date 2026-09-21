package volumes

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// TypeSpellingValidator refuses a spelling of a built-in storage class that an
// import never writes. Import writes the display alias, so any other spelling
// of the same class plans a replacement after the first import.
type TypeSpellingValidator struct{}

var _ validator.String = TypeSpellingValidator{}

func (v TypeSpellingValidator) Description(_ context.Context) string {
	return "Volume type must carry the spelling GPCN reports: \"SSD\" or \"NVMe\" for a built-in storage class, or the component code for any other class"
}

func (v TypeSpellingValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v TypeSpellingValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	volumeType := req.ConfigValue.ValueString()
	alias, diverges := reportedVolumeTypeAlias(volumeType)
	if !diverges {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		ErrSummaryInvalidVolumeType,
		fmt.Sprintf(ErrDetailVolumeTypeSpelling, volumeType, alias),
	)
}

// A class the mapping knows reaches state as its alias only. Another case of
// that alias, and the code behind it, both diverge from the reported spelling.
func reportedVolumeTypeAlias(volumeType string) (string, bool) {
	for alias, code := range volumeTypeMapping {
		if volumeType == alias {
			return "", false
		}
		if strings.EqualFold(alias, volumeType) || volumeType == code {
			return alias, true
		}
	}
	return "", false
}
