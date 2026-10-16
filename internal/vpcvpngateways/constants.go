package vpcvpngateways

import "time"

// BaseURLV1 is the VPC collection. A VPN gateway is the singleton under a VPC,
// so it carries no ID in its path.
const BaseURLV1 = "/v1/resource/vpcs/"

const gatewayPathSegment = "/vpn-gateway"

// The states a VPN gateway reports.
const (
	StateCreating = "creating"
	StateReady    = "ready"
	StateFailed   = "failed"
	StateRemoving = "removing"
)

// Gateway kinds. The data center's provider picks one. The client cannot.
const (
	KindRouter    = "router"
	KindAppliance = "appliance"
)

// RemovalPollInterval is the pause between reads while the gateway is removed.
const RemovalPollInterval = 5 * time.Second

// Action names for the job poller. Each one names the operation in the log and
// in a polling failure.
const (
	ActionCreateVpnGateway = "Create GPCN VPN Gateway"
	ActionDeleteVpnGateway = "Delete GPCN VPN Gateway"
)
