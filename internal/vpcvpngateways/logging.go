package vpcvpngateways

const (
	LogStartingCreateVpnGateway             = "Starting CreateVpnGateway for VPC ID %s"
	LogIssuedCreateVpnGatewayJob            = "Successfully issued the job to create the GPCN VPN gateway. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateVpnGateway = "Long polling completed for Create GPCN VPN Gateway - proceeding to GetVpnGateway"

	LogStartingGetVpnGateway           = "Starting GetVpnGateway for VPC ID %s"
	LogSuccessfullyRetrievedVpnGateway = "Successfully retrieved the VPN gateway of VPC ID %s"

	LogStartingDeleteVpnGateway   = "Starting DeleteVpnGateway for VPC ID %s"
	LogIssuedDeleteVpnGatewayJob  = "Successfully issued the job to delete the GPCN VPN gateway. Beginning long-polling to check the status"
	LogWaitingForVpnGatewayGone   = "Waiting for the VPN gateway of VPC ID %s to be removed"
	LogVpnGatewayStillRemoving    = "The VPN gateway of VPC ID %s is still in state %s"
	LogSuccessfullyDeletedVpnGW   = "The VPN gateway of VPC ID %s is gone"
	LogVpnGatewayAlreadyDeleted   = "The GPCN VPN gateway was already deleted"
	LogVpnGatewayNotFoundRemoving = "The GPCN VPN gateway was not found. Removing it from the state"

	LogStartingCreateGPCNVpnGateway             = "Starting Create GPCN VPN Gateway"
	LogSuccessfullyFinishedCreateGPCNVpnGateway = "Successfully finished Create GPCN VPN Gateway"
	LogStartingReadGPCNVpnGateway               = "Starting Read GPCN VPN Gateway"
	LogSuccessfullyFinishedReadGPCNVpnGateway   = "Successfully finished Read GPCN VPN Gateway"
	LogStartingUpdateGPCNVpnGateway             = "Starting Update GPCN VPN Gateway"
	LogSuccessfullyFinishedUpdateGPCNVpnGateway = "Successfully finished Update GPCN VPN Gateway"
	LogStartingDeleteGPCNVpnGateway             = "Starting Delete GPCN VPN Gateway"
	LogSuccessfullyFinishedDeleteGPCNVpnGateway = "Successfully finished Delete GPCN VPN Gateway"
	LogFailedGatewayPlansReplacement            = "The VPN gateway is in the failed state, so the plan replaces it"
)
