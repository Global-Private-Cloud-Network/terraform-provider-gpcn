package sshkeys

// Log message constants for SSH key operations
const (
	// CreateSSHKey messages
	LogStartingCreateSSHKey             = "Starting Create GPCN SSH Key"
	LogSuccessfullyFinishedCreateSSHKey = "Successfully finished Create GPCN SSH Key"

	// GetSSHKey messages
	LogStartingReadSSHKey             = "Starting Read GPCN SSH Key"
	LogSuccessfullyFinishedReadSSHKey = "Successfully finished Read GPCN SSH Key"

	// UpdateSSHKey messages
	LogStartingUpdateSSHKey             = "Starting Update GPCN SSH Key"
	LogSuccessfullyFinishedUpdateSSHKey = "Successfully finished Update GPCN SSH Key"

	// DeleteSSHKey messages
	LogStartingDeleteSSHKey             = "Starting Delete GPCN SSH Key"
	LogSuccessfullyFinishedDeleteSSHKey = "Successfully finished Delete GPCN SSH Key"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogSSHKeyNotFoundRemovingFromState = "GPCN SSH Key no longer exists (deleted outside of Terraform) - removing it from state"
	LogSSHKeyAlreadyDeleted            = "GPCN SSH Key was already deleted"
)
