package sshkeys

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateSSHKey    = "Unable to Create SSH Key"
	ErrSummaryUnableToReadSSHKey      = "Unable to Read SSH Key"
	ErrSummaryUnableToUpdateSSHKey    = "Unable to Update SSH Key"
	ErrSummaryUnableToDeleteSSHKey    = "Unable to Delete SSH Key"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadSSHKeyFailed   = "Failed to read SSH key with ID %s"
	ErrDetailUpdateSSHKeyFailed = "Failed to update SSH key with ID %s"
	ErrDetailDeleteSSHKeyFailed = "Failed to delete SSH key with ID %s"
)
