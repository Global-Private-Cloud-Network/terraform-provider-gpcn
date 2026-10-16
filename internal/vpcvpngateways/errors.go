package vpcvpngateways

import "errors"

// ErrVpnGatewayNotFound reports that the VPC has no VPN gateway. A deleted VPC
// answers the same way for its whole tree.
var ErrVpnGatewayNotFound = errors.New("the VPC has no VPN gateway")

// ErrRemovalTimeout reports that the gateway was still there when the wait ran
// out.
var ErrRemovalTimeout = errors.New("timed out waiting for the VPN gateway to be removed")

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToCreateVpnGW     = "Unable to create GPCN™ VPN gateway"
	ErrSummaryUnableToGetVpnGW        = "Unable to get GPCN™ VPN gateway"
	ErrSummaryUnableToUpdateVpnGW     = "Unable to update GPCN™ VPN gateway"
	ErrSummaryUnableToDeleteVpnGW     = "Unable to delete GPCN™ VPN gateway"
	ErrSummaryInvalidImportID         = "Invalid import ID"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient   = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNoJobIDInResponse    = "the response carried no job ID"
	ErrDetailCreateFailedNoReason = "The VPN gateway is in the failed state and GPCN™ recorded no reason. Terraform has marked it tainted, so the next apply deletes it and creates it again."
	ErrDetailImportIDFormat       = "Import ID must be the ID of the VPC that holds the VPN gateway. A VPC has one VPN gateway, so no gateway ID is needed. Got: '%s'"
	ErrDetailUpdateNotSupported   = "A VPN gateway has no attribute that updates in place. Please report this issue to the provider developers."
	ErrFmtRemovalTimeout          = "VPC %s: the VPN gateway was still in state '%s' after %s (failure reason: %s)"
	ErrFmtCreatePollingFailed     = "create VPN gateway polling failed: %w"
	ErrFmtDeletePollingFailed     = "delete VPN gateway polling failed: %w"
	ErrFmtWaitForRemovalFailed    = "wait for VPN gateway removal failed: %w"
	removalTimeoutNoReason        = "none"
)
