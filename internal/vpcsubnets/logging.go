package vpcsubnets

// Log message constants for VPC subnet operations
const (
	// CreateSubnet messages
	LogStartingCreateSubnet              = "Starting CreateSubnet"
	LogIssuedCreateSubnetJob             = "Successfully issued the job to create the GPCN VPC subnet. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateSubnet  = "Long polling completed for Create GPCN VPC Subnet - proceeding to GetSubnet"
	LogSuccessfullyRetrievedSubnetCreate = "Successfully retrieved the GPCN VPC subnet - Create"

	// GetSubnet messages
	LogStartingGetSubnetWithID           = "Starting GetSubnet for subnet ID %s"
	LogSuccessfullyRetrievedSubnetWithID = "Successfully retrieved the subnet with ID %s"
	LogSubnetAbsentFromListing           = "Subnet ID %s is on no page of the VPC subnet listing"

	// UpdateSubnet messages
	LogStartingUpdateSubnetWithID = "Starting UpdateSubnet for subnet ID %s"
	LogSuccessfullyUpdatedSubnet  = "Successfully updated the subnet with ID %s"

	// RebindSubnetNsg messages
	LogStartingRebindSubnetNsg     = "Starting RebindSubnetNsg for subnet ID %s and security group ID %s"
	LogIssuedRebindSubnetNsgJob    = "Successfully issued the job to rebind the subnet security group. Beginning long-polling to check the status"
	LogSuccessfullyRebondSubnetNsg = "Successfully rebound the security group of the subnet with ID %s"

	// DeleteSubnet messages
	LogStartingDeleteSubnetWithID  = "Starting DeleteSubnet for subnet ID %s"
	LogIssuedDeleteSubnetJob       = "Successfully issued the job to delete the GPCN VPC subnet. Beginning long-polling to check the status"
	LogSuccessfullyDeletedSubnet   = "Successfully deleted the subnet with ID %s"
	LogSubnetAlreadyDeleted        = "The GPCN VPC subnet was already deleted"
	LogSubnetNotFoundRemovingState = "The GPCN VPC subnet was not found. Removing it from the state"

	// Resource lifecycle messages
	LogStartingCreateGPCNSubnet             = "Starting Create GPCN VPC Subnet"
	LogSuccessfullyFinishedCreateGPCNSubnet = "Successfully finished Create GPCN VPC Subnet"
	LogStartingReadGPCNSubnet               = "Starting Read GPCN VPC Subnet"
	LogSuccessfullyFinishedReadGPCNSubnet   = "Successfully finished Read GPCN VPC Subnet"
	LogStartingUpdateGPCNSubnet             = "Starting Update GPCN VPC Subnet"
	LogSuccessfullyFinishedUpdateGPCNSubnet = "Successfully finished Update GPCN VPC Subnet"
	LogStartingDeleteGPCNSubnet             = "Starting Delete GPCN VPC Subnet"
	LogSuccessfullyFinishedDeleteGPCNSubnet = "Successfully finished Delete GPCN VPC Subnet"
)
