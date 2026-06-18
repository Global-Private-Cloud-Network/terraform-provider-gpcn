package datacenters

// Error summary constants
const (
	ErrSummaryUnableGetDatacenters = "Unable to get GPCN Datacenters"
)

// Error detail message templates
const (
	ErrDetailDatacenterNotFound          = "A datacenter with the provided information was not found. Some possible values are: %s"
	ErrDetailDatacenterNotFoundCountries = "A datacenter with the provided information was not found. Possible country values are: %s"
	ErrDetailDatacenterNoGPUEnabled      = "No datacenters matching the specified filters have GPU support enabled. Datacenters were found but none had gpu_enabled = true."
	ErrDetailDatacenterNoCustomImages    = "No datacenters matching the specified filters support custom images. Datacenters were found but none had custom_images = true."
)
