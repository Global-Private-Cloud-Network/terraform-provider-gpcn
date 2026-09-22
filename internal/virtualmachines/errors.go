package virtualmachines

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType             = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateVM                    = "Unable to create GPCN Virtual Machine"
	ErrSummaryRetrievingVMInfoFailed              = "Retrieving information about the Virtual Machine failed"
	ErrSummaryErrorUpdatingVMSize                 = "Error updating Virtual Machine size"
	ErrSummaryErrorUpdatingVMAttributes           = "Error updating Virtual Machine attributes"
	ErrSummaryErrorRetrievingNetworkIfaces        = "Error retrieving network interfaces"
	ErrSummaryErrorUpdatingNetworkInterfaces      = "Error updating network interfaces"
	ErrSummaryUnableToCreateDeleteRequest         = "Unable to create a request for deleting a new GPCN Virtual Machine"
	ErrSummaryUnableToDeleteVM                    = "Unable to delete GPCN Virtual Machine"
	ErrSummaryUnableToUpdateVM                    = "Unable to update GPCN Virtual Machine"
	ErrSummaryErrorReadingDeleteBody              = "Error reading body response GPCN Virtual Machine - Delete"
	ErrSummaryErrorUnmarshalingDelete             = "Error unmarshaling GPCN Virtual Machine - Delete"
	ErrSummaryEncounteredErrorGettingJobInfo      = "Encountered an error getting job info"
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
	ErrDetailNetworkInterfacesForVM          = "Error retrieving network interfaces for virtual machine with ID %s"
	ErrDetailVMInfoFailedCanImport           = "Retrieving information about the Virtual Machine failed. The job was successful, but Terraform could not read more information about its value. You can import the id to repair the state with terraform import"
	ErrDetailUnableToDeleteVMWithID          = "Unable to delete GPCN Virtual Machine with ID %s"
	ErrDetailUnmarshalingDeleteWithID        = "Error unmarshaling GPCN Virtual Machine - Delete with ID %s"
	ErrDetailJobInfoCheckDashboard           = "Encountered an error getting job info. The request may still have succeeded. Check the GPCN dashboard for more information"
	ErrDetailStoppingVM                      = "Error stopping virtual machine with ID %s"
	ErrDetailPublicIpIdConflictsWithAllocate = "public_ip_id and allocate_public_ip are mutually exclusive"
	ErrDetailFetchUpgradeSizesFailed         = "The provider could not fetch the valid upgrade targets for this Virtual Machine, so it cannot tell whether the size_id change is an in-place upgrade. This error is often transient. Re-run the plan. Underlying error: %s"
	ErrDetailNoPrimaryNetworkInterface       = "No network interface on virtual machine with ID %s is marked primary, so the public IP cannot be changed"
	ErrDetailPrimaryInterfaceNotOnAVpc       = "the primary network interface of virtual machine %s is not on a VPC subnet, and a public IP attaches to a VPC interface only"
	ErrDetailVMCreatedAttachFailed           = "virtual machine %s was created and is in state, but attaching %s failed: %s. Terraform has marked the machine tainted: run terraform untaint on it and apply again to attach the remaining networks, or let the next apply replace it."
)

// GPCN reports one address row for an address the operator attached and for the
// leftover of a failed read-back. The provider cannot tell them apart. It refuses the
// acquire and names the address, because adopting a held one makes the next destroy
// release what gpcn_vpc_public_ip owns.
const ErrDetailPrimaryInterfaceCarriesAForeignAddress = "the primary network interface of virtual machine %s already carries public IP %s, which Terraform did not acquire; name it in public_ip_id or detach it before asking for an acquired address"

// A start that fails after the provider stopped the machine leaves it stopped. The
// remedy differs by path. Create taints the machine, so the next apply replaces it.
// The tail of Update writes state, so a second apply changes nothing. A failure between
// the stop and the state write records nothing. Earlier steps of that update can still
// have succeeded, so the user reads the next plan before applying it.
const (
	ErrDetailVMLeftStoppedCreate = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal, then run terraform untaint on it; otherwise the next apply replaces the machine."
	ErrDetailVMLeftStoppedUpdate = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal."
	ErrDetailVMLeftStoppedRetry  = "virtual machine %s was stopped for the change and did not start again: %s. Start it in the portal, then run terraform plan and check the proposed changes before applying."
)

// An address the provider acquired exists at the platform even when the step that
// follows fails. No attribute records it, so the diagnostic is the only place the
// operator reads its id. The phrases below name the step that failed.
const (
	ErrDetailPublicIpOrphaned                  = "public IP %s was acquired for virtual machine %s but %s: %s. Release it in the portal or import it as gpcn_vpc_public_ip."
	ErrDetailPublicIpAttachFailedReleased      = "public IP %s was acquired for virtual machine %s but attaching it failed: %s; the address was released."
	ErrDetailPublicIpAttachFailedReleaseFailed = "public IP %s was acquired for virtual machine %s but attaching it failed: %s; releasing it failed too: %s. Release it in the portal or import it as gpcn_vpc_public_ip."
	ErrPhrasePublicIpAcquisitionFailed         = "its acquisition job failed"
	ErrPhrasePublicIpAttachFailed              = "attaching it failed"
	ErrPhrasePublicIpReleaseFailed             = "releasing it failed"
	ErrPhrasePublicIpReadBackFailed            = "reading the machine back failed"
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
