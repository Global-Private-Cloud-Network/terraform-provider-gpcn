package virtualmachines

// Log message constants for virtual machine operations
const (
	// CreateVirtualMachine messages
	LogStartingCreateVirtualMachine               = "Starting CreateVirtualMachine"
	LogValidatingPublicIPConfiguration            = "Validating public IP configuration"
	LogValidatedPublicIPConfigurationSuccessfully = "Validated public IP configuration successfully"
	LogNetworkIdsNotNull                          = "NetworkIds was not null. Adding network interfaces to create request"
	LogNetworkIdsNullOrEmpty                      = "NetworkIds was null or empty in the Virtual Machine creation. VM will be created with a default network"
	LogConstructedCreateVMRequest                 = "Constructed Create GPCN Virtual Machine request successfully"
	LogIssuedCreateVMJob                          = "Successfully issued to job to create GPCN Virtual Machine. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateVM               = "Long polling completed for Create GPCN Virtual Machine - proceeding to poll for VM status"
	LogSuccessfullyProcessedVMCreate              = "Successfully processed GPCN Virtual Machine - Create"
	LogSuccessfullyCreatedVMMayNotBeRunning       = "Creating the Virtual Machine complete. The Virtual Machine was attempted to be started, but may not be running yet. Check the GPCN Dashboard for more information"

	// GetVirtualMachine messages
	LogStartingGetVMWithID           = "Starting GetVirtualMachine for Virtual Machine ID: %s"
	LogSuccessfullyRetrievedVMWithID = "Successfully retrieved Virtual Machine with ID: %s"

	// UpdateVirtualMachine messages
	LogStartingUpdateVMWithID               = "Starting UpdateVirtualMachine for Virtual Machine ID: %s"
	LogSuccessfullyUpdatedVMWithID          = "Successfully updated Virtual Machine with ID: %s"
	LogSuccessfullyUpdatedVMMayNotBeRunning = "Updating the Virtual Machine with ID %s complete. The Virtual Machine was attempted to be started, but may not be running yet. Check the GPCN Dashboard for more information"

	// PollForVirtualMachineStatus messages
	LogStartingPollForVMStatusWithID = "Starting PollForVirtualMachineStatus for Virtual Machine ID: %s"
	LogStartingLongPollingIteration  = "Starting long polling iteration %d for retrieving information about the Virtual Machine. Seconds spent: %d"
	LogVMResponseStatus              = "Virtual Machine response status is: %s"
	LogVMStatusProceedingToAttach    = "Virtual Machine with Id %s is '%s'. Proceeding to attach networks and volumes if possible"

	// ValidatePublicIpValue messages
	LogStartingValidatePublicIPValue          = "Starting ValidatePublicIpValue"
	LogPublicIPNotAllocated                   = "Public IP not allocated, validation passed"
	LogNoNetworksSpecified                    = "No networks specified, validation passed"
	LogValidatingPublicIPSettingByNetworkType = "Validating public IP setting by checking primary network type"
	LogPublicIPValidationPassed               = "Public IP validation passed"

	// GetVirtualMachineImageId messages
	LogStartingGetVMImageIDWithName           = "Starting GetVirtualMachineImageId for image name: %s"
	LogSuccessfullyRetrievedVMImageIDWithName = "Successfully retrived virtual machine image ID for image name: %s"

	// GetVirtualMachineSizeConfigurationId messages
	LogStartingGetVMSizeIDWithName           = "Starting GetVirtualMachineSizeConfigurationId for size name: %s"
	LogSuccessfullyRetrievedVMSizeIDWithName = "Successfully retrived virtual machine size ID for size name: %s"

	// UpdateVirtualMachineSize messages
	LogStartingUpdateVMSizeWithID = "Starting UpdateVirtualMachineSize for Virtual Machine ID: %s"
	LogSuccessfullyUpdatedVMSize  = "Successfully updated Virtual Machine size"

	// ValidatePlanSizeLargerThanStateSize messages
	LogSizeChangedVerifyingLarger = "Size has changed, verifying the new size is larger than the old"

	// StartVirtualMachine messages
	LogStartingStartVMWithID       = "Starting StartVirtualMachine for Virtual Machine ID: %s"
	LogSuccessfullyStartedVMWithID = "Successfully started Virtual Machine with ID: %s"
	LogStartingIteration           = "Starting iteration %d to attempt to start virtual machine"

	// StopVirtualMachine messages
	LogStartingStopVMWithID        = "Starting StopVirtualMachine for Virtual Machine ID: %s"
	LogSuccessfullyStoppedVMWithID = "Successfully stopped Virtual Machine with ID: %s"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNVirtualMachine             = "Starting Create GPCN Virtual machine"
	LogSuccessfullyFinishedCreateGPCNVirtualMachine = "Successfully finished Create GPCN Virtual Machine"
	LogStartingReadGPCNVirtualMachine               = "Starting Read GPCN Virtual Machine"
	LogSuccessfullyFinishedReadGPCNVirtualMachine   = "Successfully finished Read GPCN Virtual Machine"
	LogStartingUpdateGPCNVirtualMachine             = "Starting Update GPCN Virtual Machine"
	LogPerformingVirtualMachineResize               = "Performing Virtual Machine resize"
	LogNameChangedUpdatingVirtualMachine            = "Name has changed, updating Virtual Machine"
	LogAllVMUpdateOpsCompleteRetrievingLatestInfo   = "All Virtual Machine update operations are completed, performing GET calls to retrieve latest info"
	LogRetrievedLatestVMInfoMappingToModel          = "Retrieved latest Virtual Machine info, now mapping to model"
	LogSuccessfullyFinishedUpdateGPCNVirtualMachine = "Successfully finished Update GPCN Virtual Machine"
	LogStartingDeleteGPCNVirtualMachine             = "Starting Delete GPCN Virtual Machine"
	LogConstructedDeleteGPCNVirtualMachineRequest   = "Constructed Delete GPCN Virtual Machine request successfully"
	LogIssuedDeleteGPCNVirtualMachineJob            = "Successfully issued job to delete GPCN Virtual Machine. Beginning long-polling to check the status"
	LogSuccessfullyFinishedDeleteGPCNVirtualMachine = "Successfully finished Delete GPCN Virtual Machine"
)
