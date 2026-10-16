package vpcvpnconnections

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// NamePattern is the pattern the API applies to a VPN connection name.
var NamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 .-]*[a-zA-Z0-9])?$`)

// PskPattern is the pattern the API publishes for a pre-shared key.
var PskPattern = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]{16,64}$`)

// PskValidator checks a pre-shared key. The framework's regex validator echoes
// the value in its message, and a diagnostic must never carry the key.
type PskValidator struct {
	// Attribute names the schema attribute the message points at.
	Attribute string
}

var _ validator.String = PskValidator{}

func (v PskValidator) Description(_ context.Context) string {
	return fmt.Sprintf(ErrDetailPskPattern, v.Attribute)
}

func (v PskValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v PskValidator) ValidateString(ctx context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}
	if PskPattern.MatchString(request.ConfigValue.ValueString()) {
		return
	}
	response.Diagnostics.AddAttributeError(request.Path, ErrSummaryInvalidPsk, v.Description(ctx))
}

// MultipleOfValidator checks that an integer divides evenly by Step.
type MultipleOfValidator struct {
	// Attribute names the schema attribute the message points at.
	Attribute string
	Step      int64
}

var _ validator.Int64 = MultipleOfValidator{}

func (v MultipleOfValidator) Description(_ context.Context) string {
	return fmt.Sprintf(ErrDetailMultipleOf, v.Attribute, v.Step)
}

func (v MultipleOfValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v MultipleOfValidator) ValidateInt64(ctx context.Context, request validator.Int64Request, response *validator.Int64Response) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}
	if request.ConfigValue.ValueInt64()%v.Step == 0 {
		return
	}
	response.Diagnostics.AddAttributeError(request.Path, fmt.Sprintf(ErrSummaryInvalidAttribute, v.Attribute),
		fmt.Sprintf("%s, got: %d", v.Description(ctx), request.ConfigValue.ValueInt64()))
}
