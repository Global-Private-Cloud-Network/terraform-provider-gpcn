package vpcpublicips

// Log message constants for VPC public IP operations
const (
	// AcquirePublicIp messages
	LogStartingAcquirePublicIp      = "Starting AcquirePublicIp for VPC ID %s"
	LogIssuedAcquirePublicIpJob     = "Successfully issued job to acquire a GPCN VPC public IP. Beginning long-polling to check the status"
	LogSuccessfullyAcquiredPublicIp = "Successfully acquired GPCN VPC public IP with ID %s"

	// GetPublicIp messages
	LogStartingGetPublicIp           = "Starting GetPublicIp for VPC ID %s and public IP ID %s"
	LogSuccessfullyRetrievedPublicIp = "Successfully retrieved GPCN VPC public IP with ID %s"

	// AttachPublicIp messages
	LogStartingAttachPublicIp       = "Starting AttachPublicIp for public IP ID %s and network interface ID %s"
	LogIssuedAttachPublicIpJob      = "Successfully issued job to attach a GPCN VPC public IP. Beginning long-polling to check the status"
	LogSuccessfullyAttachedPublicIp = "Successfully attached GPCN VPC public IP with ID %s"

	// DetachPublicIp messages
	LogStartingDetachPublicIp       = "Starting DetachPublicIp for public IP ID %s"
	LogIssuedDetachPublicIpJob      = "Successfully issued job to detach a GPCN VPC public IP. Beginning long-polling to check the status"
	LogSuccessfullyDetachedPublicIp = "Successfully detached GPCN VPC public IP with ID %s"

	// ReleasePublicIp messages
	LogStartingReleasePublicIp      = "Starting ReleasePublicIp for public IP ID %s"
	LogIssuedReleasePublicIpJob     = "Successfully issued job to release a GPCN VPC public IP. Beginning long-polling to check the status"
	LogSuccessfullyReleasedPublicIp = "Successfully released GPCN VPC public IP with ID %s"
	LogPublicIpAlreadyReleased      = "GPCN VPC public IP was already released"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNPublicIp             = "Starting Create GPCN VPC Public IP"
	LogSuccessfullyFinishedCreateGPCNPublicIp = "Successfully finished Create GPCN VPC Public IP"
	LogStartingReadGPCNPublicIp               = "Starting Read GPCN VPC Public IP"
	LogSuccessfullyFinishedReadGPCNPublicIp   = "Successfully finished Read GPCN VPC Public IP"
	LogStartingDeleteGPCNPublicIp             = "Starting Delete GPCN VPC Public IP"
	LogSuccessfullyFinishedDeleteGPCNPublicIp = "Successfully finished Delete GPCN VPC Public IP"

	LogStartingCreateGPCNPublicIpAttachment             = "Starting Create GPCN VPC Public IP Attachment"
	LogSuccessfullyFinishedCreateGPCNPublicIpAttachment = "Successfully finished Create GPCN VPC Public IP Attachment"
	LogStartingReadGPCNPublicIpAttachment               = "Starting Read GPCN VPC Public IP Attachment"
	LogSuccessfullyFinishedReadGPCNPublicIpAttachment   = "Successfully finished Read GPCN VPC Public IP Attachment"
	LogStartingDeleteGPCNPublicIpAttachment             = "Starting Delete GPCN VPC Public IP Attachment"
	LogSuccessfullyFinishedDeleteGPCNPublicIpAttachment = "Successfully finished Delete GPCN VPC Public IP Attachment"
)

// Drift-detection messages for resources changed outside of Terraform
const (
	LogPublicIpNotFoundRemovingFromState   = "GPCN VPC public IP no longer exists (released outside of Terraform) - removing it from state"
	LogAttachmentNotFoundRemovingFromState = "GPCN VPC public IP is no longer attached to a virtual machine (detached outside of Terraform) - removing the attachment from state"
)
