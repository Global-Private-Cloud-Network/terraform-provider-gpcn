package volumes

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateVolume    = "Unable to create GPCN Volume"
	ErrSummaryUnableToGetVolume       = "Unable to get GPCN Volume"
	ErrSummaryUnableToUpdateVolume    = "Unable to update GPCN Volume"
	ErrSummaryUnableToDeleteVolume    = "Unable to delete GPCN Volume"
	ErrSummaryInvalidVolumeType       = "Invalid volume type"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient         = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailNoResourceIDInJob          = "the create job succeeded but named no volume ID, so a volume may exist that Terraform does not track. Find it in the GPCN™ dashboard, then import it with terraform import or delete it there"
	ErrDetailUnableToGetVolumeWithID    = "Unable to get GPCN Volume with ID '%s'"
	ErrDetailUnableToUpdateVolumeWithID = "Unable to update GPCN Volume with ID '%s'"
	ErrDetailUnableToDeleteVolumeWithID = "Unable to delete GPCN Volume with ID '%s'"
	ErrDetailVolumeTypeSpelling         = "volume_type %q is spelled differently from what GPCN reports; use %q"
)
