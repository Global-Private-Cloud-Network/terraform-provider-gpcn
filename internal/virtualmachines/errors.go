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
)

// Warning summary constants
const (
	WarnSummaryRemovingNetworkInterfaceFailed = "Removing network interface failed"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient        = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNetworkInterfacesForNewVM = "Error retrieving network interfaces for newly created virtual machine with ID %s"
	ErrDetailNetworkInterfacesForVM    = "Error retrieving network interfaces for virtual machine with ID %s"
	ErrDetailVMInfoFailedCanImport     = "Retrieving information about the Virtual Machine failed. The job was successful, but Terraform could not read more information about its value. You can import the id to repair the state with terraform import"
	ErrDetailAddedNetworksExceedsMax   = "this change would exceed the maximum number of networks attached allowed %d"
	ErrDetailUnableToDeleteVMWithID    = "Unable to delete GPCN Virtual Machine with ID %s"
	ErrDetailUnmarshalingDeleteWithID  = "Error unmarshaling GPCN Virtual Machine - Delete with ID %s"
	ErrDetailJobInfoCheckDashboard     = "Encountered an error getting job info. The request may still have succeeded. Check the GPCN dashboard for more information"
	ErrDetailStoppingVM                = "Error stopping virtual machine with ID %s"
	ErrDetailStartingVM                = "Error starting virtual machine with ID %s"
	ErrDetailCannotRemoveLastNetwork   = "unable to remove the last Network attached to a virtual machine"
	ErrDetailNetworkTypeMustBeStandard = "the prospective primary network (first in the list) is of type custom. The value for allocatePublicIp can only be set to true if the primary network's network_type is standard"
)

// Warning detail message templates
const (
	WarnDetailRemovingNetworkInterfaceWithIDFailed = "Removing the network interface with ID '%s' failed"
)

// Polling constants
const (
	ErrVirtualMachineStatusTimeoutTemplate         = "After %d seconds, the virtual machine was still not in the target status. Please check the GPCN API for more information"
	ErrVirtualMachineStatusPollInterruptedTemplate = "waiting for virtual machine %s to reach its target status was interrupted: %w"
)
