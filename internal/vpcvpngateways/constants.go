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

const RemovalPollInterval = 5 * time.Second

const (
	ActionCreateVpnGateway = "Create GPCN VPN Gateway"
	ActionDeleteVpnGateway = "Delete GPCN VPN Gateway"
)
