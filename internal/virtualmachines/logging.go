package virtualmachines

// Log message constants for virtual machine operations
const (
	// CreateVirtualMachine messages
	LogStartingCreateVirtualMachine  = "Starting CreateVirtualMachine"
	LogConstructedCreateVMRequest    = "Constructed Create GPCN Virtual Machine request successfully"
	LogIssuedCreateVMJob             = "Successfully issued to job to create GPCN Virtual Machine. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateVM  = "Long polling completed for Create GPCN Virtual Machine - proceeding to poll for VM status"
	LogSuccessfullyProcessedVMCreate = "Successfully processed GPCN Virtual Machine - Create"

	// GetVirtualMachine messages
	LogStartingGetVMWithID           = "Starting GetVirtualMachine for Virtual Machine ID %s"
	LogSuccessfullyRetrievedVMWithID = "Successfully retrieved Virtual Machine with ID %s"

	// UpdateVirtualMachine messages
	LogStartingUpdateVMWithID               = "Starting UpdateVirtualMachine for Virtual Machine ID %s"
	LogSuccessfullyUpdatedVMWithID          = "Successfully updated Virtual Machine with ID %s"
	LogSuccessfullyUpdatedVMMayNotBeRunning = "Updating the Virtual Machine with ID %s complete. The Virtual Machine was attempted to be started, but may not be running yet. Check the GPCN Dashboard for more information"

	// PollForVirtualMachineStatus messages
	LogStartingPollForVMStatusWithID = "Starting PollForVirtualMachineStatus for Virtual Machine ID %s"
	LogInitialPollDelay              = "Waiting %d seconds before starting status polling"
	LogStartingLongPollingIteration  = "Starting long polling iteration %d for retrieving information about the Virtual Machine. Seconds spent: %d"
	LogVMResponseStatus              = "Virtual Machine response status is: %s"
	LogVMStatusProceedingToAttach    = "Virtual Machine with ID %s is '%s'. Proceeding to attach networks and volumes if possible"

	// UpdateVirtualMachineSize messages
	LogStartingUpdateVMSizeWithID = "Starting UpdateVirtualMachineSize for Virtual Machine ID %s"
	LogSuccessfullyUpdatedVMSize  = "Successfully updated Virtual Machine size"

	// StartVirtualMachine messages
	LogStartingStartVMWithID       = "Starting StartVirtualMachine for Virtual Machine ID %s"
	LogSuccessfullyStartedVMWithID = "Successfully started Virtual Machine with ID %s"

	// StopVirtualMachine messages
	LogStartingStopVMWithID        = "Starting StopVirtualMachine for Virtual Machine ID %s"
	LogSuccessfullyStoppedVMWithID = "Successfully stopped Virtual Machine with ID %s"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNVirtualMachine             = "Starting Create GPCN Virtual machine"
	LogSuccessfullyFinishedCreateGPCNVirtualMachine = "Successfully finished Create GPCN Virtual Machine"
	LogStartingReadGPCNVirtualMachine               = "Starting Read GPCN Virtual Machine"
	LogSuccessfullyFinishedReadGPCNVirtualMachine   = "Successfully finished Read GPCN Virtual Machine"
	LogStartingUpdateGPCNVirtualMachine             = "Starting Update GPCN Virtual Machine"
	LogPerformingVirtualMachineResize               = "Performing Virtual Machine resize"
	LogVirtualMachineAlreadyCarriesTheSize          = "The Virtual Machine already carries the planned size, skipping the resize"
	LogAttributesChangedUpdatingVirtualMachine      = "Attributes have changed, updating Virtual Machine"
	LogAllVMUpdateOpsCompleteRetrievingLatestInfo   = "All Virtual Machine update operations are completed, performing GET calls to retrieve latest info"
	LogRetrievedLatestVMInfoMappingToModel          = "Retrieved latest Virtual Machine info, now mapping to model"
	LogSuccessfullyFinishedUpdateGPCNVirtualMachine = "Successfully finished Update GPCN Virtual Machine"
	LogStartingDeleteGPCNVirtualMachine             = "Starting Delete GPCN Virtual Machine"
	LogConstructedDeleteGPCNVirtualMachineRequest   = "Constructed Delete GPCN Virtual Machine request successfully"
	LogIssuedDeleteGPCNVirtualMachineJob            = "Successfully issued job to delete GPCN Virtual Machine. Beginning long-polling to check the status"
	LogSuccessfullyFinishedDeleteGPCNVirtualMachine = "Successfully finished Delete GPCN Virtual Machine"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogVirtualMachineNotFoundRemovingFromState = "GPCN Virtual Machine no longer exists (deleted outside of Terraform) - removing it from state"
	LogVirtualMachineAlreadyDeleted            = "GPCN Virtual Machine was already deleted"
	LogVirtualMachineTerminalRemovingFromState = "The GPCN platform already removed the Virtual Machine - removing it from state: %s"
)
