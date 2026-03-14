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

	// CheckAlgorithms messages
	LogStartingCheckAlgorithms    = "Starting CheckAlgorithms for algorithm %s"
	LogAlgorithmCheckPassed       = "Algorithm %s is supported by the API" //nolint:gosec // G101: Not a credential, this is a log message template
	LogAlgorithmsRetrievedFromAPI = "Successfully retrieved supported algorithms from the API"
)
