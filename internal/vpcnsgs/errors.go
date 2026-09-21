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
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient      = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetNsgWithID    = "Unable to get GPCN VPC security group with ID '%s'"
	ErrDetailUnableToUpdateNsgWithID = "Unable to update GPCN VPC security group with ID '%s'"
	ErrDetailUnableToDeleteNsgWithID = "Unable to delete GPCN VPC security group with ID '%s'"
	ErrDetailImportIDFormat          = "Import ID must be '<vpc_id>/<nsg_id>'. A security group is read through its VPC, so the VPC ID cannot be derived from the group ID alone. Got: '%s'"
)

// Rule refusal details. Each one is the sentence GPCN answers with, so the plan
// and the apply refuse a rule in the same words.
const (
	ErrDetailRulePortsNotApplicable = "ports are not applicable to protocol '%s'"
	ErrDetailRulePortsTogether      = "portRangeMin and portRangeMax must be provided together"
	ErrDetailRulePortOrder          = "portRangeMin must not exceed portRangeMax"
)

// Warning strings for a rules replace against the VPC's own default group. The
// two quoted sentences are the descriptions GPCN stages with the VPC.
const (
	WarnSummaryDefaultNsgRulesReplaced = "Replacing the rules of the VPC default security group"
	WarnDetailDefaultNsgRulesReplaced  = "Security group '%s' is the VPC's own default group. GPCN replaces the whole rule set on every change, so this apply deletes the two rules the VPC was born with: 'Default: allow all outbound traffic' and 'Default: allow traffic from this VPC'. Add them to the configuration to keep them."
)
