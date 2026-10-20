package vpcvpnconnections

// None of these messages carries a request body, because the body holds the
// pre-shared key.
const (
	LogStartingCreateVpnConnection        = "Starting CreateVpnConnection for VPC ID %s"
	LogIssuedCreateVpnConnectionJob       = "Successfully issued the job to create the GPCN VPN connection. Beginning long-polling to check the status"
	LogStartingFindVpnConnection          = "Starting FindVpnConnectionByName for VPC ID %s"
	LogStartingGetVpnConnection           = "Starting GetVpnConnection for VPC ID %s, VPN connection ID %s"
	LogSuccessfullyRetrievedVpnConnection = "Successfully retrieved VPN connection ID %s"
	LogStartingUpdateVpnConnection        = "Starting UpdateVpnConnection for VPN connection ID %s"
	LogIssuedUpdateVpnConnectionJob       = "Successfully issued the job to update the GPCN VPN connection. Beginning long-polling to check the status"
	LogStartingDeleteVpnConnection        = "Starting DeleteVpnConnection for VPN connection ID %s"
	LogIssuedDeleteVpnConnectionJob       = "Successfully issued the job to delete the GPCN VPN connection. Beginning long-polling to check the status"

	LogStartingCreateGPCNVpnConnection             = "Starting Create GPCN VPN Connection"
	LogSuccessfullyFinishedCreateGPCNVpnConnection = "Successfully finished Create GPCN VPN Connection"
	LogStartingReadGPCNVpnConnection               = "Starting Read GPCN VPN Connection"
	LogSuccessfullyFinishedReadGPCNVpnConnection   = "Successfully finished Read GPCN VPN Connection"
	LogStartingUpdateGPCNVpnConnection             = "Starting Update GPCN VPN Connection"
	LogSuccessfullyFinishedUpdateGPCNVpnConnection = "Successfully finished Update GPCN VPN Connection"
	LogStartingDeleteGPCNVpnConnection             = "Starting Delete GPCN VPN Connection"
	LogSuccessfullyFinishedDeleteGPCNVpnConnection = "Successfully finished Delete GPCN VPN Connection"
	LogVpnConnectionNotFoundRemoving               = "The GPCN VPN connection was not found. Removing it from the state"
	LogVpnConnectionAlreadyDeleted                 = "The GPCN VPN connection was already deleted"
	LogVpnGatewayGoneKeepingID                     = "The VPN gateway of the VPC was not found. Keeping vpn_gateway_id from the state"
	LogAdoptedVpnConnectionByName                  = "The POST ended without a usable answer, but the VPN connection exists. Adopting it by name"
	LogRowNotCreatedByThisRequest                  = "The VPN connection with this name does not come from this request, so the create does not adopt it"
	LogDeleteJobFailedWaiting                      = "The delete job failed. Waiting to see whether the VPN connection goes or parks"
	LogParkedRemovalResent                         = "The removal of the VPN connection parked with a failure reason. Sending the delete again"
	LogRemovalAlreadyRunning                       = "The API answered that the removal of the VPN connection already runs. Waiting for it to finish"
	LogWaitingForVpnConnectionGone                 = "Waiting for VPN connection ID %s to be removed"
	LogVpnConnectionStillRemoving                  = "VPN connection ID %s is still in state %s"
	LogUnappliedPlansUpdate                        = "The VPN connection settings are not running, so the plan updates it"
	LogFailedWithoutUpdatePlansReplacement         = "The VPN connection failed and the API refuses an update, so the plan replaces it"
	LogUpdateSendsNoRequest                        = "No attribute that the API stores changed. Reading the VPN connection without a PUT"
)
