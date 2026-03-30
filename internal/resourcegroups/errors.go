package resourcegroups

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType     = "Unexpected Configure Type"
	ErrSummaryUnableToCreateResourceGroup = "Unable to Create Resource Group"
	ErrSummaryUnableToReadResourceGroup   = "Unable to Read Resource Group"
	ErrSummaryUnableToUpdateResourceGroup = "Unable to Update Resource Group"
	ErrSummaryUnableToDeleteResourceGroup = "Unable to Delete Resource Group"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient        = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadResourceGroupFailed   = "Failed to read resource group with ID %s"
	ErrDetailUpdateResourceGroupFailed = "Failed to update resource group with ID %s"
	ErrDetailDeleteResourceGroupFailed = "Failed to delete resource group with ID %s"
)
