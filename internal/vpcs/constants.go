package vpcs

var BASE_URL_V1 string = "/v1/resource/vpcs/"

// VPC states (src/components/vpc/vpc.types.ts)
var VPC_STATUS_CREATING = "creating"
var VPC_STATUS_ACTIVE = "active"
var VPC_STATUS_FAILED = "failed"
var VPC_STATUS_DELETING = "deleting"

// API error codes this resource branches on (src/common/errorCodes.ts)
var ERROR_CODE_CIDR_OVERLAP_UNCONFIRMED = "VPC_CIDR_OVERLAP_UNCONFIRMED"
var ERROR_CODE_VPC_NOT_EMPTY = "VPC_NOT_EMPTY"
var ERROR_CODE_VPC_NOT_ACTIVE = "VPC_NOT_ACTIVE"

// Job actions, used in polling log lines and in the failure sentence
var ACTION_CREATE_VPC = "Create GPCN VPC"
var ACTION_DELETE_VPC = "Delete GPCN VPC"
