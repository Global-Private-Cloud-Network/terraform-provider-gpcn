package virtualmachines

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType             = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCompletePlan                = "Unable to complete plan"
	ErrSummaryErrorVerifyingImage                 = "Error verifying the virtual image"
	ErrSummaryErrorVerifyingSize                  = "Error verifying the size"
	ErrSummaryUnableToCreateVM                    = "Unable to create GPCN Virtual Machine"
	ErrSummaryRetrievingVMInfoFailed              = "Retrieving information about the Virtual Machine failed"
	ErrSummaryErrorUpdatingVMSize                 = "Error updating Virtual Machine size"
	ErrSummaryErrorUpdatingVMName                 = "Error updating Virtual Machine name"
	ErrSummaryErrorRetrievingNetworkIfaces        = "Error retrieving network interfaces"
	ErrSummaryErrorUpdatingNetworkInterfaces      = "Error updating network interfaces"
	ErrSummaryErrorUpdatingVolumes                = "Error updating volumes"
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
	WarnSummaryAttachingVolumeFailed          = "Attaching volume failed"
	WarnSummaryRemovingNetworkInterfaceFailed = "Removing network interface failed"
	WarnSummaryRemovingVolumeFailed           = "Removing volume failed"
	WarnSummaryUnableToStartVM                = "Unable to start GPCN Virtual Machine"
)

// Error detail message templates
const (
	ErrDetailExpectedHTTPClient                 = "Expected *http.Client, got: %T. Please report this issue to the provider developers."
	ErrDetailSizeNoLongerAvailable              = "The size in the state is no longer available for this datacenter and image. This will require a re-create"
	ErrDetailSizeNotAvailableForDatacenterImage = "The size '%s' is not available for this datacenter and image. The available values are: %s"
	ErrDetailImageVerificationFailed            = "Error verifying the virtual image: '%s' for datacenter with ID: '%s'"
	ErrDetailSizeVerificationFailed             = "Error verifying the size: '%s' for datacenter with ID: '%s'"
	ErrDetailNetworkInterfacesForNewVM          = "Error retrieving network interfaces for newly created virtual machine with ID: '%s'"
	ErrDetailNetworkInterfacesForVM             = "Error retrieving network interfaces for virtual machine with ID: '%s'"
	ErrDetailVMInfoFailedCanImport              = "Retrieving information about the Virtual Machine failed. The job was successful, but Terraform could not read more information about its value. You can import the id to repair the state with terraform import"
	ErrDetailAddedNetworksExceedsMax            = "this change would exceed the maximum number of networks attached allowed %d"
	ErrDetailUnableToDeleteVMWithID             = "Unable to delete GPCN Virtual Machine with ID '%s'"
	ErrDetailUnmarshalingDeleteWithID           = "Error unmarshaling GPCN Virtual Machine - Delete with ID '%s'"
	ErrDetailJobInfoCheckDashboard              = "Encountered an error getting job info. The request may still have succeeded. Check the GPCN dashboard for more information"
	ErrDetailStoppingVM                         = "Error stopping virtual machine with ID: '%s'"
	ErrDetailStartingVM                         = "Error starting virtual machine with ID: '%s'"
	ErrDetailCannotRemoveLastNetwork            = "unable to remove the last Network attached to a virtual machine"
	ErrDetailNetworkTypeMustBeStandard          = "the prospective primary network (first in the list) is of type custom. The value for allocatePublicIp can only be set to true if the primary network's network_type is standard"
)

// Warning detail message templates
const (
	WarnDetailAttachingVolumeWithIDFailed          = "Attaching volume with ID: '%s' failed"
	WarnDetailRemovingNetworkInterfaceWithIDFailed = "Removing the network interface with ID: '%s' failed"
	WarnDetailRemovingVolumeWithIDFailed           = "Removing the volume with ID: '%s' failed"
)

// Polling constants
const (
	ErrVirtualMachineStatusTimeout = "After 5 minutes, the virtual machine was still not in the target status. Please check the GPCN API for more information"
)
