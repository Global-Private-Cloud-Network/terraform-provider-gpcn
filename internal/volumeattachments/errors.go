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
	ErrDetailVMStopFailed          = "virtual machine %s could not be stopped: %s"
	ErrDetailVMStartFailed         = "virtual machine %s could not be started after volume operation: %s"
)
