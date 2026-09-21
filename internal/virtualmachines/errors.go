package virtualmachines

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType             = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCompletePlan                = "Unable to complete plan"
	ErrSummaryUnableToCreateVM                    = "Unable to create GPCN Virtual Machine"
	ErrSummaryRetrievingVMInfoFailed              = "Retrieving information about the Virtual Machine failed"
	ErrSummaryErrorUpdatingVMSize                 = "Error updating Virtual Machine size"
	ErrSummaryErrorUpdatingVMAttributes           = "Error updating Virtual Machine attributes"
	ErrSummaryErrorRetrievingNetworkIfaces        = "Error retrieving network interfaces"
	ErrSummaryErrorUpdatingNetworkInterfaces      = "Error updating network interfaces"
	ErrSummaryUnableToCreateDeleteRequest         = "Unable to create a request for deleting a new GPCN Virtual Machine"
	ErrSummaryUnableToDeleteVM                    = "Unable to delete GPCN Virtual Machine"
	ErrSummaryUnableToUpdateVM                    = "Unable to update GPCN Virtual Machine"
	ErrSummaryUnableToStopVM                      = "Unable to stop GPCN Virtual Machine"
	ErrSummaryErrorReadingDeleteBody              = "Error reading body response GPCN Virtual Machine - Delete"
	ErrSummaryErrorUnmarshalingDelete             = "Error unmarshaling GPCN Virtual Machine - Delete"
	ErrSummaryEncounteredErrorGettingJobInfo      = "Encountered an error getting job info"
	ErrSummaryEncounteredValidationError          = "Encountered a validation error"
	ErrSummaryUnableToUpdatePublicIPConfiguration = "Unable to update public IP configuration"
	ErrSummaryUnableToDetermineSizeChange         = "Unable to determine whether size_id change requires replacement"
	ErrSummaryNoPrimaryNetworkInterface           = "No primary network interface"
	ErrSummaryPublicIpConflict                    = "Invalid public IP configuration"
	ErrSummaryVMCreatedAttachFailed               = "Virtual machine created but a network interface attach failed"
	ErrSummaryVMLeftStopped                       = "Virtual machine left stopped"
)

// Warning summary constants
const (
	WarnSummaryRemovingNetworkInterfaceFailed = "Removing network interface failed"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient              = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNetworkInterfacesForNewVM       = "Error retrieving network interfaces for newly created virtual machine with ID %s"
	ErrDetailNetworkInterfacesForVM          = "Error retrieving network interfaces for virtual machine with ID %s"
	ErrDetailVMInfoFailedCanImport           = "Retrieving information about the Virtual Machine failed. The job was successful, but Terraform could not read more information about its value. You can import the id to repair the state with terraform import"
	ErrDetailUnableToDeleteVMWithID          = "Unable to delete GPCN Virtual Machine with ID %s"
	ErrDetailUnmarshalingDeleteWithID        = "Error unmarshaling GPCN Virtual Machine - Delete with ID %s"
	ErrDetailJobInfoCheckDashboard           = "Encountered an error getting job info. The request may still have succeeded. Check the GPCN dashboard for more information"
	ErrDetailStoppingVM                      = "Error stopping virtual machine with ID %s"
	ErrDetailStartingVM                      = "Error starting virtual machine with ID %s"
	ErrDetailPublicIpIdConflictsWithAllocate = "public_ip_id and allocate_public_ip are mutually exclusive"
	ErrDetailFetchUpgradeSizesFailed         = "The provider could not fetch the valid upgrade targets for this Virtual Machine, so it cannot tell whether the size_id change is an in-place upgrade. This error is often transient. Re-run the plan. Underlying error: %s"
	ErrDetailNoPrimaryNetworkInterface       = "No network interface on virtual machine with ID %s is marked primary, so the public IP cannot be changed"
	ErrDetailVMCreatedAttachFailed           = "virtual machine %s was created and is in state, but attaching %s failed: %s. Terraform has marked the machine tainted: run terraform untaint on it and apply again to attach the remaining networks, or let the next apply replace it."
)

// A start that fails after the provider stopped the machine leaves it stopped. The
// remedy differs by path. Create taints the machine, so the next apply replaces it.
// The tail of Update writes state, so a second apply changes nothing. A failure between
// the stop and the state write records nothing, so the next apply retries the change.
const (
	ErrDetailVMLeftStoppedCreate = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal, then run terraform untaint on it; otherwise the next apply replaces the machine."
	ErrDetailVMLeftStoppedUpdate = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal."
	ErrDetailVMLeftStoppedRetry  = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal; the change was not recorded, so the next apply retries it."
)

// Warning detail message templates
const (
	WarnDetailRemovingNetworkInterfaceWithIDFailed = "Removing the network interface with ID '%s' failed"
)

// Polling constants
const (
	ErrVirtualMachineStatusTimeoutTemplate = "After %d seconds, the virtual machine was still not in the target status. Please check the GPCN API for more information"
	ErrDetailVMTerminalStatus              = "virtual machine %s reached status %q while waiting for %s"
)
