package vpcnsgs

// Log message constants for VPC security group operations
const (
	// CreateNsg messages
	LogStartingCreateNsg              = "Starting CreateNsg"
	LogIssuedCreateNsgJob             = "Successfully issued the job to create the GPCN VPC security group. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateNsg  = "Long polling completed for Create GPCN VPC Security Group - proceeding to GetNsg"
	LogSuccessfullyRetrievedNsgCreate = "Successfully retrieved the GPCN VPC security group - Create"

	// GetNsg messages
	LogStartingGetNsgWithID           = "Starting GetNsg for security group ID %s"
	LogSuccessfullyRetrievedNsgWithID = "Successfully retrieved the security group with ID %s"

	// RenameNsg messages
	LogStartingRenameNsgWithID   = "Starting RenameNsg for security group ID %s"
	LogSuccessfullyRenamedNsg    = "Successfully renamed the security group with ID %s"
	LogStartingReplaceNsgRules   = "Starting ReplaceNsgRules for security group ID %s"
	LogIssuedReplaceNsgRulesJob  = "Successfully issued the job to replace the security group rules. Beginning long-polling to check the status"
	LogSuccessfullyReplacedRules = "Successfully replaced the rules of the security group with ID %s"

	// DeleteNsg messages
	LogStartingDeleteNsgWithID  = "Starting DeleteNsg for security group ID %s"
	LogIssuedDeleteNsgJob       = "Successfully issued the job to delete the GPCN VPC security group. Beginning long-polling to check the status"
	LogSuccessfullyDeletedNsg   = "Successfully deleted the security group with ID %s"
	LogNsgAlreadyDeleted        = "The GPCN VPC security group was already deleted"
	LogNsgNotFoundRemovingState = "The GPCN VPC security group was not found. Removing it from the state"

	// Resource lifecycle messages
	LogStartingCreateGPCNNsg             = "Starting Create GPCN VPC Security Group"
	LogSuccessfullyFinishedCreateGPCNNsg = "Successfully finished Create GPCN VPC Security Group"
	LogStartingReadGPCNNsg               = "Starting Read GPCN VPC Security Group"
	LogSuccessfullyFinishedReadGPCNNsg   = "Successfully finished Read GPCN VPC Security Group"
	LogStartingUpdateGPCNNsg             = "Starting Update GPCN VPC Security Group"
	LogSuccessfullyFinishedUpdateGPCNNsg = "Successfully finished Update GPCN VPC Security Group"
	LogStartingDeleteGPCNNsg             = "Starting Delete GPCN VPC Security Group"
	LogSuccessfullyFinishedDeleteGPCNNsg = "Successfully finished Delete GPCN VPC Security Group"
)
