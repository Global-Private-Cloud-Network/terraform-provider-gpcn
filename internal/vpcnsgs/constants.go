package vpcnsgs

// BaseURLV1 is the VPC collection. Every security group route is scoped under
// a VPC. A group carries no datacenter and no resource group of its own.
const BaseURLV1 = "/v1/resource/vpcs/"

const (
	nsgsPathSegment  = "/nsgs/"
	rulesPathSegment = "/rules"
)

// Rule vocabularies. The API stores the wildcard protocol as "all".
var (
	RuleDirections = []string{"ingress", "egress"}
	RuleProtocols  = []string{"tcp", "udp", "icmp", "all"}
)

// Action names for the job poller. Each one names the operation in the log and
// in a polling failure.
const (
	ActionCreateNsg       = "Create GPCN VPC Security Group"
	ActionReplaceNsgRules = "Replace GPCN VPC Security Group Rules"
	ActionDeleteNsg       = "Delete GPCN VPC Security Group"
)
