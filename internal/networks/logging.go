package networks

// Log message constants for network operations
const (
	// CreateNetwork messages
	LogStartingCreateNetwork               = "Starting CreateNetwork"
	LogConstructedCreateNetworkRequestBody = "Constructed Create GPCN Network request body successfully"
	LogConstructedCreateNetworkRequest     = "Constructed Create GPCN Network request successfully"
	LogIssuedCreateNetworkJob              = "Successfully issued to job to create GPCN Network. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateNetwork   = "Long polling completed for Create GPCN Network - proceeding to GetNetwork"
	LogSuccessfullyRetrievedNetworkCreate  = "Successfully retrieved GPCN Network - Create"

	// GetNetwork messages
	LogStartingGetNetworkWithID           = "Starting GetNetwork for network ID: %s"
	LogSuccessfullyRetrievedNetworkWithID = "Successfully retrieved network with ID: %s"

	// GetVirtualMachinesAttachedToNetworks messages
	LogStartingGetVirtualMachinesAttachedToNetworks           = "Starting GetVirtualMachinesAttachedToNetworks for network ID: %s"
	LogSuccessfullyRetrievedVirtualMachinesAttachedToNetworks = "Successfully retrieved virtual machines attached to network with ID: %s"

	// AllocatePublicIp messages
	LogStartingAllocatePublicIp      = "Starting AllocatePublicIp for virtualmachine ID: %s and network interface ID: %s"
	LogSuccessfullyAllocatedPublicIp = "Successfully allocated a public IP address for virtualmachine ID: %s and network interface ID: %s"

	// ReleasePublicIp messages
	LogStartingReleasePublicIp      = "Starting ReleasePublicIp for virtualmachine ID: %s and network interface ID: %s"
	LogSuccessfullyReleasedPublicIp = "Successfully released the public IP address for virtualmachine ID: %s and network interface ID: %s"

	// UpdateNetwork messages
	LogStartingUpdateNetworkWithID         = "Starting UpdateNetwork for network ID: %s"
	LogConstructedUpdateNetworkRequestBody = "Constructed Update GPCN Network request body successfully"
	LogConstructedUpdateNetworkRequest     = "Constructed Update GPCN Network request successfully"
	LogUpdateRequestSentSuccessfully       = "Update request sent successfully - proceeding to GetNetwork"
	LogSuccessfullyRetrievedNetworkUpdate  = "Successfully retrieved GPCN Network - Update"

	// DeleteNetwork messages
	LogStartingDeleteNetworkWithID              = "Starting DeleteNetwork for network ID: %s"
	LogConstructedDeleteNetworkRequest          = "Constructed Delete GPCN Network request successfully"
	LogIssuedDeleteNetworkJob                   = "Successfully issued job to delete GPCN Network. Beginning long-polling to check the status"
	LogSuccessfullyCompletedDeleteNetworkWithID = "Successfully completed DeleteNetwork for network ID: %s"
	LogDeleteNetworkFailedRetrying              = "Delete GPCN Network failed for network ID: %s. Issuing retry number: %d. Max retries allowed: %d"

	// GetNetworkInterfaces messages
	LogStartingGetNetworkInterfacesWithID        = "Starting GetNetworkInterfaces for Virtual Machine ID: %s"
	LogSuccessfullyRetrievedAllNetworkInterfaces = "Successfully retrieved all network interfaces for Virtual Machine ID: %s"

	// AddNetworkInterface messages
	LogStartingAddNetworkInterfaceWithIDs   = "Starting AddNetworkInterface for Virtual Machine ID: %s with network ID: %s"
	LogSuccessfullyAttachedNetworkInterface = "Successfully attached network interface"

	// SetNextNetworkInterfaceToPrimary messages
	LogStartingSetNextNetworkInterfaceToPrimary = "Starting SetNextNetworkInterfaceToPrimary for Virtual Machine ID: %s"
	LogSettingNetworkInterfaceAsPrimary         = "Setting network interface with ID %s as primary"
	LogSuccessfullySetNetworkInterfaceAsPrimary = "Successfully set network interface with ID %s as primary"

	// RemoveNetworkInterface messages
	LogStartingRemoveNetworkInterfaceWithIDs = "Starting RemoveNetworkInterface for Virtual Machine ID: %s with network interface ID: %s"
	LogSuccessfullyRemovedNetworkInterface   = "Successfully removed network interface with ID: %s"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNNetwork             = "Starting Create GPCN Network"
	LogSuccessfullyFinishedCreateGPCNNetwork = "Successfully finished Create GPCN Network"
	LogStartingReadGPCNNetwork               = "Starting Read GPCN Network"
	LogSuccessfullyRetrievedGPCNNetworkRead  = "Successfully retrieved GPCN Network - Read"
	LogSuccessfullyFinishedReadGPCNNetwork   = "Successfully finished Read GPCN Network"
	LogStartingUpdateGPCNNetwork             = "Starting Update GPCN Network"
	LogSuccessfullyFinishedUpdateGPCNNetwork = "Successfully finished Update GPCN Network"
	LogStartingDeleteGPCNNetwork             = "Starting Delete GPCN Network"
	LogSuccessfullyFinishedDeleteGPCNNetwork = "Successfully finished Delete GPCN Network"
)
