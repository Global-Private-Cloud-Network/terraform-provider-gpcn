package gpu

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateGPU       = "Unable to Create GPU"
	ErrSummaryUnableToReadGPU         = "Unable to Read GPU"
	ErrSummaryUnableToUpdateGPU       = "Unable to Update GPU"
	ErrSummaryUnableToDeleteGPU       = "Unable to Delete GPU"
	ErrSummaryUnableToFetchInventory  = "Unable to fetch GPU inventory"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient         = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNoResourceIDInJob          = "the create job succeeded but named no GPU ID, so a GPU may exist that Terraform does not track. Find it in the GPCN™ dashboard, then import it with terraform import or delete it there"
	ErrDetailReadGPUFailed              = "Failed to read GPU with ID %s"
	ErrDetailUpdateGPUFailed            = "Failed to update GPU with ID %s"
	ErrDetailDeleteGPUFailed            = "Failed to delete GPU with ID %s"
	ErrDetailNoInventoryAvailable       = "no GPU availability for series code %s in datacenter %s with GPU count %d"
	ErrDetailNoInventoryAvailableByName = "no GPU availability for series %s in datacenter %s with GPU count %d"
	ErrDetailUnknownGPUSeries           = "GPU series %q is not offered in datacenter %s; available series: %s"
)
