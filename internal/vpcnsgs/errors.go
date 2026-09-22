package vpcnsgs

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToCreateNsg       = "Unable to create GPCN VPC security group"
	ErrSummaryUnableToGetNsg          = "Unable to get GPCN VPC security group"
	ErrSummaryUnableToUpdateNsg       = "Unable to update GPCN VPC security group"
	ErrSummaryUnableToDeleteNsg       = "Unable to delete GPCN VPC security group"
	ErrSummaryInvalidImportID         = "Invalid import ID"
	ErrSummaryInvalidRule             = "Invalid security group rule"
	ErrSummaryInvalidNsgAttribute     = "Invalid security group %s"
	ErrSummaryInvalidNsgRuleAttribute = "Invalid security group rule %s"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient      = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetNsgWithID    = "Unable to get GPCN VPC security group with ID '%s'"
	ErrDetailUnableToUpdateNsgWithID = "Unable to update GPCN VPC security group with ID '%s'"
	ErrDetailUnableToDeleteNsgWithID = "Unable to delete GPCN VPC security group with ID '%s'"
	ErrDetailNsgCreatedJobFailed     = "security group %s was created and is in state, but its creation job failed: %s. Terraform has marked the group tainted, so the next apply deletes it and creates it again."
	ErrDetailImportIDFormat          = "Import ID must be '<vpc_id>/<nsg_id>'. A security group is read through its VPC, so the VPC ID cannot be derived from the group ID alone. Got: '%s'"
)

// Rule refusal details. Each one is the sentence GPCN answers with. The plan
// and the apply then refuse a rule in the same words.
const (
	ErrDetailRulePortsNotApplicable = "ports are not applicable to protocol '%s'"
	ErrDetailRulePortsTogether      = "portRangeMin and portRangeMax must be provided together"
	ErrDetailRulePortOrder          = "portRangeMin must not exceed portRangeMax"
)

// Warning strings for a group GPCN parked. The platform reconciles a parked
// group back to ready once the rules apply, so the remedy names that first.
const (
	WarnSummaryNsgFailed        = "Security group is in the failed state"
	WarnDetailNsgFailed         = "Security group %s is in the failed state: %s. Apply its rules again or delete the group and create it again."
	WarnDetailNsgFailedNoReason = "Security group %s is in the failed state. Apply its rules again or delete the group and create it again."
)

// Warning strings for a rules replace against the VPC's own default group. The
// two quoted sentences are the descriptions GPCN stages with the VPC. GPCN
// removes only the rows the desired set leaves out, so the sentence promises no
// deletion the configuration prevents.
const (
	WarnSummaryDefaultNsgRulesReplaced = "Replacing the rules of the VPC default security group"
	WarnDetailDefaultNsgRulesReplaced  = "Security group '%s' is the VPC's own default group. GPCN replaces the whole rule set on every change, so this apply deletes any rule this configuration does not list, including the two GPCN staged with the VPC: 'Default: allow all outbound traffic' and 'Default: allow traffic from this VPC'. Add them to the configuration to keep them."
)
