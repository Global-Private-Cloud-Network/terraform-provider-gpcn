package sshkeys

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateSSHKey    = "Unable to Create SSH Key"
	ErrSummaryUnableToReadSSHKey      = "Unable to Read SSH Key"
	ErrSummaryUnableToUpdateSSHKey    = "Unable to Update SSH Key"
	ErrSummaryUnableToDeleteSSHKey    = "Unable to Delete SSH Key"
	ErrSummaryMissingRequiredAttr     = "Missing Required Attribute"
	ErrSummaryConflictingAttr         = "Conflicting Attribute"
	ErrSummaryUnsupportedAlgorithm    = "Unsupported Algorithm"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient            = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadSSHKeyFailed              = "Failed to read SSH key with ID %s"
	ErrDetailUpdateSSHKeyFailed            = "Failed to update SSH key with ID %s"
	ErrDetailDeleteSSHKeyFailed            = "Failed to delete SSH key with ID %s"
	ErrDetailAlgorithmRequiredForGenerate  = "'algorithm' is required when 'public_key' is not specified (generate mode)"
	ErrDetailAlgorithmConflictsWithUpload  = "'algorithm' cannot be set when 'public_key' is specified (upload mode)"
	ErrDetailPassphraseConflictsWithUpload = "'passphrase' cannot be set when 'public_key' is specified (upload mode)"
	ErrDetailAlgorithmNotSupported         = "algorithm %q is not in the list of supported algorithms returned by the API: %v"
	ErrDetailAlgorithmsCheckFailed         = "Failed to retrieve supported algorithms from the API"
	ErrDetailMalformedAlgorithmsResponse   = "malformed algorithms response: missing data"
)
