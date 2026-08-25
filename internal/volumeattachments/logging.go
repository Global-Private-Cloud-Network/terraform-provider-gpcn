package volumeattachments

const (
	LogStartingCreateVolumeAttachment = "Starting Create GPCN Volume Attachment"
	LogStartingReadVolumeAttachment   = "Starting Read GPCN Volume Attachment"
	LogStartingDeleteVolumeAttachment = "Starting Delete GPCN Volume Attachment"
	LogVolumeAlreadyAttached          = "Volume already attached to target VM, treating as success"
	LogStoppingVMBeforeAttach         = "Stopping VM before volume operation (network_hotplug disabled)"
	LogStartingVMAfterAttach          = "Starting VM after volume operation"
	LogSkippingStopVMAlreadyStopped   = "VM already stopped, skipping stop (possible concurrent operation)"
	LogSkippingStartVMAlreadyRunning  = "VM already running, skipping start (possible concurrent operation)"
	LogVolumeAttachmentNotFound       = "Volume is not attached to expected VM; removing from state"
	LogBestEffortStartFailed          = "Best-effort VM restart after failed volume operation also failed"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogVolumeAttachmentVolumeGone      = "The volume backing this attachment no longer exists - removing the attachment from state"
	LogVolumeAttachmentAlreadyDetached = "GPCN Volume Attachment was already detached - treating delete as successful"
)
