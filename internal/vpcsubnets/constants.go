package vpcsubnets

// BaseURLV1 is the VPC collection. Every subnet route is scoped under a VPC,
// because a subnet carries no datacenter and no resource group of its own.
const BaseURLV1 = "/v1/resource/vpcs/"

const (
	subnetsPathSegment = "/subnets/"
	nsgPathSegment     = "/nsg"
)

// StateFailed is the state a subnet lands in when its carve job stops badly.
// The row stays readable, so only a warning tells the operator about it.
const StateFailed = "failed"

// ListPageLimit is the largest page the subnet listing serves.
const ListPageLimit = 100

// The allocator accepts only these prefix lengths.
const (
	MinPrefix = 22
	MaxPrefix = 28
)

// Action names for the job poller. Each one names the operation in the log and
// in a polling failure.
const (
	ActionCreateSubnet = "Create GPCN VPC Subnet"
	ActionRebindNsg    = "Rebind GPCN VPC Subnet Security Group"
	ActionDeleteSubnet = "Delete GPCN VPC Subnet"
)
