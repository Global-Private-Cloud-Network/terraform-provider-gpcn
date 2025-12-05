package volumes

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToCreateVolume    = "Unable to create GPCN Volume"
	ErrSummaryUnableToGetVolume       = "Unable to get GPCN Volume"
	ErrSummaryUnableToUpdateVolume    = "Unable to update GPCN Volume"
	ErrSummaryUnableToDeleteVolume    = "Unable to delete GPCN Volume"
)

// Error detail message templates
const (
	ErrDetailExpectedHTTPClient         = "Expected *http.Client, got: %T. Please report this issue to the provider developers."
	ErrDetailUnableToGetVolumeWithID    = "Unable to get GPCN Volume with ID %s"
	ErrDetailUnableToUpdateVolumeWithID = "Unable to update GPCN Volume with ID %s"
	ErrDetailUnableToDeleteVolumeWithID = "Unable to delete GPCN Volume with ID %s"
)
