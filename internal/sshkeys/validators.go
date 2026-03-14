package sshkeys

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// AlgorithmValidator validates cross-field constraints for the algorithm attribute:
//   - When public_key is not set (generate mode), algorithm must be provided.
//   - When public_key is set (upload mode), algorithm must not be provided.
type AlgorithmValidator struct{}

func (v AlgorithmValidator) Description(_ context.Context) string {
	return "Ensures 'algorithm' is set when 'public_key' is not specified, and is not set when 'public_key' is specified."
}

func (v AlgorithmValidator) MarkdownDescription(_ context.Context) string {
	return "Ensures `algorithm` is set when `public_key` is not specified, and is not set when `public_key` is specified."
}

func (v AlgorithmValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	var config ResourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	uploadMode := !config.PublicKey.IsNull() && config.PublicKey.ValueString() != ""

	if uploadMode && !req.ConfigValue.IsNull() && req.ConfigValue.ValueString() != "" {
		resp.Diagnostics.AddError(
			ErrSummaryConflictingAttr,
			ErrDetailAlgorithmConflictsWithUpload,
		)
		return
	}

	if !uploadMode && (req.ConfigValue.IsNull() || req.ConfigValue.ValueString() == "") {
		resp.Diagnostics.AddError(
			ErrSummaryMissingRequiredAttr,
			ErrDetailAlgorithmRequiredForGenerate,
		)
	}
}

// PassphraseValidator validates that passphrase is not set when public_key is provided (upload mode).
type PassphraseValidator struct{}

func (v PassphraseValidator) Description(_ context.Context) string {
	return "Ensures 'passphrase' is not set when 'public_key' is specified (upload mode)."
}

func (v PassphraseValidator) MarkdownDescription(_ context.Context) string {
	return "Ensures `passphrase` is not set when `public_key` is specified (upload mode)."
}

func (v PassphraseValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	var config ResourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	uploadMode := !config.PublicKey.IsNull() && config.PublicKey.ValueString() != ""

	if uploadMode && !req.ConfigValue.IsNull() && req.ConfigValue.ValueString() != "" {
		resp.Diagnostics.AddError(
			ErrSummaryConflictingAttr,
			ErrDetailPassphraseConflictsWithUpload,
		)
	}
}
