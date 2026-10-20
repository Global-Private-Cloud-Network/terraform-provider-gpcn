package vpcvpngateways

import "time"

// BaseURLV1 is the VPC collection. The gateway path carries no gateway ID.
const BaseURLV1 = "/v1/resource/vpcs/"

const gatewayPathSegment = "/vpn-gateway"

const (
	StateCreating = "creating"
	StateReady    = "ready"
	StateFailed   = "failed"
	StateRemoving = "removing"
)

const (
	KindRouter    = "router"
	KindAppliance = "appliance"
)

// RemovalPollInterval is a variable so that tests can shorten the wait.
var RemovalPollInterval = 5 * time.Second

// AdoptWindow is how far before the POST a row may start and still count as
// the row of this request. It absorbs the difference between the two clocks.
const AdoptWindow = 2 * time.Minute

// CodeGatewayRemoving is the code of the 409 for a gateway whose removal runs.
const CodeGatewayRemoving = "VPN_GATEWAY_REMOVING"

// CodeGatewayBusyCreating is the code of the 409 for a gateway whose create
// still runs.
const CodeGatewayBusyCreating = "VPN_GATEWAY_BUSY_CREATING"

const (
	ActionCreateVpnGateway = "Create GPCN VPN Gateway"
	ActionDeleteVpnGateway = "Delete GPCN VPN Gateway"
)
