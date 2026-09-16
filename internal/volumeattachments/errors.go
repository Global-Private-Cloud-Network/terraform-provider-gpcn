package volumeattachments

const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Resource Configure Type"
	ErrSummaryUnableToAttachVolume    = "Unable to attach volume"
	ErrSummaryUnableToDetachVolume    = "Unable to detach volume"
	ErrSummaryUnableToReadAttachment  = "Unable to read volume attachment"
	ErrSummaryUnableToStopVM          = "Unable to stop virtual machine before volume operation"
	ErrSummaryUnableToStartVM         = "Unable to start virtual machine after volume operation"

	ErrDetailExpectedGpcnClient    = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailVolumeAlreadyAttached = "volume %s is already attached to virtual machine %s, which is different from the requested virtual machine %s"
	// Only fmt.Errorf takes this constant, because %w wraps the cause.
	ErrDetailVMStopFailed = "virtual machine %s could not be stopped: %w"
	// Only fmt.Errorf takes this constant. The %w keeps a not-found read visible
	// to client.IsNotFound, so Delete still treats a deleted VM as detached.
	ErrDetailVMReadFailed = "virtual machine %s could not be read: %w"
	// Delete reads a not-found error as "already detached". A restart failure must not
	// wrap the cause. A wrapped 404 from the start call makes Delete report a false success.
	ErrDetailVMStartFailed = "virtual machine %s could not be started after volume operation: %s"
)
