package datacenters

// Error summary constants
const (
	ErrSummaryUnableGetDatacenters = "Unable to get GPCN Datacenters"
)

// Error detail message templates
const (
	ErrDetailDatacenterNotFound       = "A datacenter with the provided information was not found. Some possible values are: %s"
	ErrDetailDatacenterNoneVisible    = "A datacenter with the provided information was not found, and this API key can see no datacenters at all."
	ErrDetailDatacenterNoGPUEnabled   = "No datacenters matching the specified filters have gpu_enabled = %t. Datacenters were found, but none matched that value."
	ErrDetailDatacenterNoCustomImages = "No datacenters matching the specified filters have custom_images = %t. Datacenters were found, but none matched that value."
)
