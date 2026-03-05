package gpu

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateGPU       = "Unable to Create GPU"
	ErrSummaryUnableToReadGPU         = "Unable to Read GPU"
	ErrSummaryUnableToUpdateGPU       = "Unable to Update GPU"
	ErrSummaryUnableToDeleteGPU       = "Unable to Delete GPU"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient           = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailReadGPUFailed                = "Failed to read GPU with ID %s"
	ErrDetailUpdateGPUFailed              = "Failed to update GPU with ID %s"
	ErrDetailDeleteGPUFailed              = "Failed to delete GPU with ID %s"
	ErrDetailMalformedResponseMissingData = "malformed inventory response: missing Data"
	ErrDetailNoInventoryAvailable         = "no GPU availability for series code %s in datacenter %s with GPU count %d"
)
