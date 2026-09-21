package vpcs

// Log message constants for VPC operations
const (
	// CreateVpc messages
	LogStartingCreateVpc       = "Starting CreateVpc"
	LogIssuedCreateVpcJob      = "Successfully issued the job to create the GPCN VPC"
	LogCreateVpcJobCompleted   = "Long polling completed for Create GPCN VPC - proceeding to GetVpc"
	LogStartingCreateGPCNVpc   = "Starting Create GPCN VPC"
	LogFinishedCreateGPCNVpc   = "Successfully finished Create GPCN VPC"
	LogVpcCreatedBeforePolling = "Wrote the GPCN VPC ID to state before polling the create job"

	// GetVpc messages
	LogStartingGetVpcWithID           = "Starting GetVpc for VPC ID %s"
	LogSuccessfullyRetrievedVpcWithID = "Successfully retrieved the VPC with ID %s"
	LogStartingReadGPCNVpc            = "Starting Read GPCN VPC"
	LogFinishedReadGPCNVpc            = "Successfully finished Read GPCN VPC"
	LogVpcNotFoundRemovingFromState   = "GPCN VPC was not found - removing it from state"

	// UpdateVpc messages
	LogStartingUpdateVpcWithID = "Starting UpdateVpc for VPC ID %s"
	LogVpcUpdateNothingToSend  = "No name or description change for VPC ID %s - skipping the update request"
	LogStartingUpdateGPCNVpc   = "Starting Update GPCN VPC"
	LogFinishedUpdateGPCNVpc   = "Successfully finished Update GPCN VPC"

	// DeleteVpc messages
	LogStartingDeleteVpcWithID  = "Starting DeleteVpc for VPC ID %s"
	LogIssuedDeleteVpcJob       = "Successfully issued the job to delete the GPCN VPC"
	LogVpcActiveJobPollFailed   = "The create job for VPC ID %s stopped before the delete retry: %s"
	LogWaitingForVpcActiveJob   = "VPC ID %s is still creating - waiting for job %s before the delete retry"
	LogStartingDeleteGPCNVpc    = "Starting Delete GPCN VPC"
	LogFinishedDeleteGPCNVpc    = "Successfully finished Delete GPCN VPC"
	LogVpcAlreadyDeleted        = "GPCN VPC was already deleted outside of Terraform"
	LogSuccessfullyDeletedVpcID = "Successfully completed the delete of VPC ID %s"
)
