package resourcegroups

var BASE_URL_V1 string = "/v1/resource-groups/"

const (
	ErrorCodeResourceGroupIsDefault = "RESOURCE_GROUP_IS_DEFAULT"
	ErrorCodeResourceGroupNotEmpty  = "RESOURCE_GROUP_NOT_EMPTY"
)

// The page size keeps each request small. The page cap bounds the whole read.
const (
	listLimit   = 100
	listPageCap = 100
)
