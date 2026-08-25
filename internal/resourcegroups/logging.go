package resourcegroups

// Log message constants for resource group operations
const (
	// CreateResourceGroup messages
	LogStartingCreateResourceGroup             = "Starting Create GPCN Resource Group"
	LogSuccessfullyFinishedCreateResourceGroup = "Successfully finished Create GPCN Resource Group"

	// GetResourceGroup messages
	LogStartingReadResourceGroup             = "Starting Read GPCN Resource Group"
	LogSuccessfullyFinishedReadResourceGroup = "Successfully finished Read GPCN Resource Group"

	// UpdateResourceGroup messages
	LogStartingUpdateResourceGroup             = "Starting Update GPCN Resource Group"
	LogSuccessfullyFinishedUpdateResourceGroup = "Successfully finished Update GPCN Resource Group"

	// DeleteResourceGroup messages
	LogStartingDeleteResourceGroup             = "Starting Delete GPCN Resource Group"
	LogSuccessfullyFinishedDeleteResourceGroup = "Successfully finished Delete GPCN Resource Group"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogResourceGroupNotFoundRemovingFromState = "GPCN Resource Group no longer exists (deleted outside of Terraform) - removing it from state"
	LogResourceGroupAlreadyDeleted            = "GPCN Resource Group was already deleted - treating delete as successful"
)
