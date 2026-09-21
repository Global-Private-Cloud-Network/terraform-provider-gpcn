package volumeattachments

const (
	LogStartingCreateVolumeAttachment = "Starting Create GPCN Volume Attachment"
	LogStartingReadVolumeAttachment   = "Starting Read GPCN Volume Attachment"
	LogStartingDeleteVolumeAttachment = "Starting Delete GPCN Volume Attachment"
	LogVolumeAlreadyAttached          = "Volume already attached to target VM, treating as success"
	LogVolumeAttachmentNotFound       = "Volume is not attached to expected VM; removing from state"
)

// Drift-detection messages for resources deleted outside of Terraform
const (
	LogVolumeAttachmentVolumeGone      = "The volume backing this attachment no longer exists - removing the attachment from state"
	LogVolumeAttachmentAlreadyDetached = "GPCN Volume Attachment was already detached - treating delete as successful"
)
