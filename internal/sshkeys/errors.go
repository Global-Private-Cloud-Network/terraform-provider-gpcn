package sshkeys

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateSSHKey    = "Unable to Create SSH Key"
	ErrSummaryUnableToReadSSHKey      = "Unable to Read SSH Key"
	ErrSummaryUnableToUpdateSSHKey    = "Unable to Update SSH Key"
	ErrSummaryUnableToDeleteSSHKey    = "Unable to Delete SSH Key"
	ErrSummaryInvalidSSHKeyName       = "Invalid SSH key name"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadSSHKeyFailed   = "Failed to read SSH key with ID %s"
	ErrDetailUpdateSSHKeyFailed = "Failed to update SSH key with ID %s"
	ErrDetailDeleteSSHKeyFailed = "Failed to delete SSH key with ID %s"
)

// GPCN sends the last three of these details verbatim, so they stay byte-exact.
const (
	ErrDetailSSHKeyNameWhitespace = "Name must not start or end with whitespace (GPCN trims it, which would make the stored name differ from the configuration)"
	ErrDetailSSHKeyNameRequired   = "Name is required"
	ErrDetailSSHKeyNameTooLong    = "Name must be at most %d characters"
	ErrDetailSSHKeyNameCharacters = "Name may contain only letters, numbers, spaces, periods, hyphens, and the symbols _ ( ) ' #, and must begin and end with a letter or number"
)
