package vpcvpnconnections

import "time"

// BaseURLV1 is the VPC collection. A VPN connection lives under its VPC.
const BaseURLV1 = "/v1/resource/vpcs/"

const connectionsPathSegment = "/vpn-connections"

// The states a VPN connection reports.
const (
	StateCreating     = "creating"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
	StateError        = "error"
	StateFailed       = "failed"
	StateRemoving     = "removing"
)

// The defaults the API fills for a parameter the configuration omits. The
// schema defaults carry the same values, so the plan matches the API.
const (
	DefaultIkeVersion           = "v2"
	DefaultEncryption           = "aes256"
	DefaultIntegrity            = "sha256"
	DefaultDhGroup              = 14
	DefaultPfsGroup             = 14
	DefaultIkeLifetimeSeconds   = 28800
	DefaultIpsecLifetimeSeconds = 3600
	DefaultDpd                  = true
)

// Bounds the API enforces.
const (
	NameMinLength     = 1
	NameMaxLength     = 63
	DescriptionMax    = 500
	PeerCidrsMin      = 1
	PeerCidrsMax      = 10
	PeerCidrMinPrefix = 8
	LifetimeMin       = 60
	LifetimeMax       = 86400
	LifetimeStep      = 60
)

// Action names for the job poller. Each one names the operation in the log and
// in a polling failure.
const (
	ActionCreateVpnConnection = "Create GPCN VPN Connection"
	ActionUpdateVpnConnection = "Update GPCN VPN Connection"
	ActionDeleteVpnConnection = "Delete GPCN VPN Connection"
)

// The keys of the request bodies.
const (
	keyName                 = "name"
	keyDescription          = "description"
	keyPeerAddress          = "peerAddress"
	keyPeerCidrs            = "peerCidrs"
	keyPsk                  = "psk"
	keyIkeVersion           = "ikeVersion"
	keyEncryption           = "encryption"
	keyIntegrity            = "integrity"
	keyDhGroup              = "dhGroup"
	keyPfsGroup             = "pfsGroup"
	keyIkeLifetimeSeconds   = "ikeLifetimeSeconds"
	keyIpsecLifetimeSeconds = "ipsecLifetimeSeconds"
	keyDpd                  = "dpd"
)

// ImportIDSeparator joins the VPC ID and the connection ID in an import ID.
const ImportIDSeparator = "/"

// The list route pages at 100 rows at most. A VPC holds a handful of
// connections, so the page cap only stops a loop that never ends.
const (
	listPageSize = 100
	listMaxPages = 20
)

// PrivateKeyUpdateRefused is the private state key that Read sets to "true" when
// the connection failed and the API no longer accepts an update.
const (
	PrivateKeyUpdateRefused = "update_refused"
	VerbUpdate              = "update"
)

// PrivateKeyJobActive is the private state key that Read sets to "true" when the
// connection runs a job.
const PrivateKeyJobActive = "job_active"

// CodeStateConflict is the code of the 409 for a connection whose state refuses
// the call.
const CodeStateConflict = "VPN_STATE_CONFLICT"

// RemovalPollInterval is a variable so that tests can shorten the wait.
var RemovalPollInterval = 5 * time.Second
