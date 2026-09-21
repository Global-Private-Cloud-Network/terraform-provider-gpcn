package sshkeys

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const sshKeyNameMaxLength = 30

// The pattern is the one GPCN builds for SSH keys. See naming.ts makeNameSchema
// with the extra characters _()'#.
var sshKeyNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 .\-_()'#]*[a-zA-Z0-9])?$`)

// NameValidator rejects an SSH key name that GPCN rejects, and also one that
// GPCN silently trims.
type NameValidator struct{}

var _ validator.String = NameValidator{}

func (v NameValidator) Description(_ context.Context) string {
	return fmt.Sprintf("Name must be 1 to %d characters, drawn from letters, numbers, spaces, periods, hyphens and the symbols _ ( ) ' #, must begin and end with a letter or number, and must not start or end with whitespace", sshKeyNameMaxLength)
}

func (v NameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v NameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	name := req.ConfigValue.ValueString()

	// GPCN trims the name before it validates and stores it. A name with outer
	// whitespace therefore comes back different and makes the plan never settle.
	if strings.TrimSpace(name) != name {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidSSHKeyName, ErrDetailSSHKeyNameWhitespace)
		return
	}
	if name == "" {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidSSHKeyName, ErrDetailSSHKeyNameRequired)
		return
	}
	if utf8.RuneCountInString(name) > sshKeyNameMaxLength {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidSSHKeyName, fmt.Sprintf(ErrDetailSSHKeyNameTooLong, sshKeyNameMaxLength))
		return
	}
	if !sshKeyNamePattern.MatchString(name) {
		resp.Diagnostics.AddAttributeError(req.Path, ErrSummaryInvalidSSHKeyName, ErrDetailSSHKeyNameCharacters)
	}
}
