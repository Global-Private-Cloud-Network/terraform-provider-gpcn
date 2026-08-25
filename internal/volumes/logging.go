package volumes

// Log message constants for volume operations
const (
	// CreateVolume messages
	LogStartingCreateVolume              = "Starting CreateVolume"
	LogLookingUpVolumeSkuId              = "Looking up volume SKU ID for validation"
	LogConstructedCreateVolumeRequest    = "Constructed Create GPCN Volume request successfully"
	LogIssuedCreateVolumeJob             = "Successfully issued to job to create GPCN Volume. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateVolume  = "Long polling completed for Create GPCN Volume - proceeding to GetVolume"
	LogSuccessfullyRetrievedVolumeCreate = "Successfully retrieved GPCN Volume - Create"

	// GetVolume messages
	LogStartingGetVolumeWithID           = "Starting GetVolume for volume ID %s"
	LogSuccessfullyRetrievedVolumeWithID = "Successfully retrieved volume with ID %s"

	// ListVolumesByName messages
	LogStartingListVolumesByName       = "Starting ListVolumesByName for volume name: %s"
	LogSuccessfullyListedVolumesByName = "Successfully retrieved volume with name: %s"

	// UpdateVolume messages
	LogStartingUpdateVolumeWithID        = "Starting UpdateVolume for volume ID %s"
	LogValidatingVolumeSkuIdForUpdate    = "Validating volume SKU ID for update"
	LogConstructedUpdateVolumeRequest    = "Constructed Update GPCN Volume request successfully"
	LogIssuedUpdateVolumeJob             = "Successfully issued to job to update GPCN Volume. Beginning long-polling to check the status"
	LogLongPollingCompletedUpdateVolume  = "Long polling completed for Update GPCN Volume - proceeding to GetVolume"
	LogSuccessfullyRetrievedVolumeUpdate = "Successfully retrieved GPCN Volume - Update"

	// DeleteVolume messages
	LogStartingDeleteVolumeWithID              = "Starting DeleteVolume for volume ID %s"
	LogConstructedDeleteVolumeRequest          = "Constructed Delete GPCN Volume request successfully"
	LogIssuedDeleteVolumeJob                   = "Successfully issued job to delete GPCN Volume. Beginning long-polling to check the status"
	LogSuccessfullyCompletedDeleteVolumeWithID = "Successfully completed DeleteVolume for volume ID %s"

	// GetVolumeSkuId messages
	LogStartingGetVolumeSkuIdWithParams           = "Starting GetVolumeSkuId for component code %s and size: %s"
	LogValidatingVolumeTypeAvailable              = "Validating volume type is available"
	LogValidatingVolumeSizeAvailable              = "Validating volume size is available"
	LogSuccessfullyRetrievedVolumeSkuIdWithParams = "Successfully retrieved volume SKU ID for component code %s and size: %s"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNVolume             = "Starting Create GPCN Volume"
	LogSuccessfullyFinishedCreateGPCNVolume = "Successfully finished Create GPCN Volume"
	LogStartingReadGPCNVolume               = "Starting Read GPCN Volume"
	LogSuccessfullyFinishedReadGPCNVolume   = "Successfully finished Read GPCN Volume"
	LogStartingUpdateGPCNVolume             = "Starting Update GPCN Volume"
	LogSuccessfullyFinishedUpdateGPCNVolume = "Successfully finished Update GPCN Volume"
	LogStartingDeleteGPCNVolume             = "Starting Delete GPCN Volume"
	LogSuccessfullyFinishedDeleteGPCNVolume = "Successfully finished Delete GPCN Volume"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogVolumeNotFoundRemovingFromState = "GPCN Volume no longer exists (deleted outside of Terraform) - removing it from state"
	LogVolumeAlreadyDeleted            = "GPCN Volume was already deleted - treating delete as successful"
)
