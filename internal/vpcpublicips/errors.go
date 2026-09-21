package vpcpublicips

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToAcquirePublicIp = "Unable to acquire GPCN VPC public IP"
	ErrSummaryUnableToGetPublicIp     = "Unable to get GPCN VPC public IP"
	ErrSummaryUnableToReleasePublicIp = "Unable to release GPCN VPC public IP"
	ErrSummaryUnableToAttachPublicIp  = "Unable to attach GPCN VPC public IP"
	ErrSummaryUnableToDetachPublicIp  = "Unable to detach GPCN VPC public IP"
)

// Error detail message templates
//
//nolint:gosec // G101: These are diagnostic sentences about an address, not a key.
const (
	ErrDetailExpectedGpcnClient            = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetPublicIpWithID     = "Unable to get GPCN VPC public IP with ID '%s' in VPC '%s'"
	ErrDetailUnableToReleasePublicIpWithID = "Unable to release GPCN VPC public IP with ID '%s' in VPC '%s'"
	ErrDetailUnableToAttachPublicIpWithID  = "Unable to attach GPCN VPC public IP with ID '%s' to network interface '%s'"
	ErrDetailUnableToDetachPublicIpWithID  = "Unable to detach GPCN VPC public IP with ID '%s'"
	ErrDetailAcquiredPublicIpJobFailed     = "public IP %s was acquired and is in state, but its acquisition job failed: %s. Terraform has marked the address tainted, so the next apply releases it and acquires another."
	ErrDetailPublicIpNotFound              = "Public IP not found"
	ErrDetailPublicIpListingTruncated      = "the public IP listing of VPC '%[2]s' still reported more rows after %[1]d pages, so the address could not be found or ruled out"
)
