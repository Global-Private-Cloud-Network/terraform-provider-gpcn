package volumes

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateVolume    = "Unable to create GPCN Volume"
	ErrSummaryUnableToGetVolume       = "Unable to get GPCN Volume"
	ErrSummaryUnableToUpdateVolume    = "Unable to update GPCN Volume"
	ErrSummaryUnableToDeleteVolume    = "Unable to delete GPCN Volume"

	ErrSummaryUnexpectedVolumeTypeValue = "Unexpected volume type value"
)

// Error detail message templates
const (
	ErrDetailExpectedGpcnClient         = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetVolumeWithID    = "Unable to get GPCN Volume with ID '%s'"
	ErrDetailUnableToUpdateVolumeWithID = "Unable to update GPCN Volume with ID '%s'"
	ErrDetailUnableToDeleteVolumeWithID = "Unable to delete GPCN Volume with ID '%s'"

	ErrDetailUnexpectedVolumeTypeValue = "Expected a volumes.VolumeTypeValue, got: %T. Please report this issue to the provider developers."
	ErrDetailUnexpectedStringValue     = "expected a basetypes.StringValue, got %T"
	ErrDetailVolumeTypeConversion      = "cannot convert a string to a volume type value: %v"
)
