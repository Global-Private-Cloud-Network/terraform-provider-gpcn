package vpcvpnconnections

import "errors"

// ErrVpnConnectionNotFound reports a 404 for the VPN connection. The API gives
// the same answer for an absent, a removed and a hidden connection.
var ErrVpnConnectionNotFound = errors.New("the VPN connection was not found")

// ErrRemovalTimeout reports a removal that the API still shows after the wait.
var ErrRemovalTimeout = errors.New("timed out waiting for the VPN connection to be removed")

const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToCreateVpnConn   = "Unable to create GPCN™ VPN connection"
	ErrSummaryUnableToGetVpnConn      = "Unable to get GPCN™ VPN connection"
	ErrSummaryUnableToUpdateVpnConn   = "Unable to update GPCN™ VPN connection"
	ErrSummaryUnableToDeleteVpnConn   = "Unable to delete GPCN™ VPN connection"
	ErrSummaryInvalidImportID         = "Invalid import ID"
	ErrSummaryInvalidPsk              = "Invalid pre-shared key"
	ErrSummaryInvalidAttribute        = "Invalid VPN connection %s"
	WarnSummaryDesiredNotApplied      = "GPCN™ VPN connection settings are not running"
)

const (
	ErrDetailExpectedGpcnClient   = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNoJobIDInResponse    = "the response carried no job ID"
	ErrDetailNoConnectionID       = "the response carried no VPN connection ID"
	ErrDetailWrongGateway         = "vpn_gateway_id %s is not the VPN gateway of VPC %s"
	ErrDetailCreateFailedNoReason = "The VPN connection is in the failed state and GPCN™ recorded no reason. Terraform has marked it tainted, so the next apply deletes it and creates it again."
	ErrDetailImportIDFormat       = "Import ID must be '<vpc_id>/<connection_id>', the ID of the VPC and the ID of the VPN connection joined by a slash. Got: '%s'"
	ErrDetailPskPattern           = "%s must be 16–64 printable ASCII characters with no spaces, double quotes or backslashes"
	ErrFmtUtf16Max                = "Attribute %s must be at most %d characters (counted in UTF-16 code units)"
	ErrDetailMultipleOf           = "%s must be a multiple of %d"
	ErrFmtNotOwnRow               = "A VPN connection named %s already exists on this VPC that this apply didn't create. Import it with: terraform import gpcn_vpc_vpn_connection.<name> %s/%s"
	ErrFmtMayCommitLater          = "GPCN™ may still finish creating this connection. Before you apply again, check this VPC for a connection named %s. If it's there, import it with: terraform import gpcn_vpc_vpn_connection.<name> %s/<connection_id>"
	ErrDetailUpdateMayStillApply  = "GPCN™ may still be applying this change. Wait for it to finish before you apply again. The next plan shows any setting it didn't apply."
	ErrDetailLookupFailed         = "A VPN connection may have been created; check the VPC and import it if so."
	ErrFmtWaitForRemovalFailed    = "wait for VPN connection removal failed: %w"
	ErrFmtRemovalTimeout          = "VPN connection %s: still in state '%s' after %s"
	ErrFmtRemovalParkedAgain      = "%s\n\nThe provider asked GPCN™ to remove VPN connection %s a second time, and that removal failed too"
	ErrFmtRemovalParkedNoTime     = "%s\n\nThe provider had too little time left to ask GPCN™ to remove VPN connection %s again. Run the destroy again to retry it"
	ErrFmtCreatePollingFailed     = "create VPN connection polling failed: %w"
	ErrFmtUpdatePollingFailed     = "update VPN connection polling failed: %w"
	ErrFmtDeletePollingFailed     = "delete VPN connection polling failed: %w"
)

// WarnDetailDesiredNotApplied is the W1 string.
const WarnDetailDesiredNotApplied = "The VPN connection's saved settings are not yet running at the data center. Apply again to retry."
