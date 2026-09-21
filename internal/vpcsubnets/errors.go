package vpcsubnets

import "errors"

// ErrSubnetAbsent reports that the subnet is on no page of its VPC's listing.
// The listing is the only read this API offers for a subnet. Absence is how a
// deletion outside Terraform arrives.
var ErrSubnetAbsent = errors.New("the subnet is not in the VPC's subnet listing")

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToCreateSubnet    = "Unable to create GPCN VPC subnet"
	ErrSummaryUnableToGetSubnet       = "Unable to get GPCN VPC subnet"
	ErrSummaryUnableToUpdateSubnet    = "Unable to update GPCN VPC subnet"
	ErrSummaryUnableToDeleteSubnet    = "Unable to delete GPCN VPC subnet"
	ErrSummaryInvalidImportID         = "Invalid import ID"
	ErrSummaryInvalidSubnetAttribute  = "Invalid GPCN VPC subnet attribute"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient         = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetSubnetWithID    = "Unable to get GPCN VPC subnet with ID '%s'"
	ErrDetailUnableToUpdateSubnetWithID = "Unable to update GPCN VPC subnet with ID '%s'"
	ErrDetailUnableToDeleteSubnetWithID = "Unable to delete GPCN VPC subnet with ID '%s'"
	ErrDetailImportIDFormat             = "Import ID must be '<vpc_id>/<subnet_id>'. A subnet is read through its VPC's listing, so the VPC ID cannot be derived from the subnet ID alone. Got: '%s'"
	ErrDetailOuterWhitespace            = "%s must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"
)

// Warning strings for a subnet the platform could not carve.
const (
	WarnSummarySubnetFailed         = "GPCN VPC subnet is in the failed state"
	WarnDetailSubnetFailed          = "Subnet '%s' is in the 'failed' state, so it carries no working network. GPCN gives this reason: %s. Delete the subnet and create it again, or contact GPCN support."
	WarnDetailSubnetNoFailureReason = "the API reported no reason"
)
