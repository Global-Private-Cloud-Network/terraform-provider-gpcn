package gpu

// Log message constants for GPU operations
const (
	// CreateGPU messages
	LogStartingCreateGPU             = "Starting Create GPCN GPU"
	LogIssuedCreateGPUJob            = "Issued Create GPU job successfully"
	LogLongPollingCompletedCreateGPU = "Long polling completed for Create GPU"
	LogSuccessfullyFinishedCreateGPU = "Successfully finished Create GPCN GPU"

	// GetGPU messages
	LogStartingReadGPU             = "Starting Read GPCN GPU"
	LogSuccessfullyFinishedReadGPU = "Successfully finished Read GPCN GPU"

	// UpdateGPU messages
	LogStartingUpdateGPU             = "Starting Update GPCN GPU"
	LogSuccessfullyFinishedUpdateGPU = "Successfully finished Update GPCN GPU"

	// DeleteGPU messages
	LogStartingDeleteGPU             = "Starting Delete GPCN GPU"
	LogIssuedDeleteGPUJob            = "Issued Delete GPU job successfully"
	LogSuccessfullyFinishedDeleteGPU = "Successfully finished Delete GPCN GPU"

	// CheckInventory messages
	LogStartingCheckInventory               = "Starting CheckInventory for series code %s in datacenter %s with GPU count %d"
	LogConstructedInventoryRequestURL       = "Constructed GPU inventory request URL successfully"
	LogSuccessfullyRetrievedInventory       = "Successfully retrieved GPU inventory response"
	LogValidatingInventoryResponseStructure = "Validating inventory response structure"
	LogInventoryAvailable                   = "GPU inventory available for series code %s in datacenter %s with GPU count %d"
)
