package l2segments

// Log message constants for L2 segment operations
const (
	// CreateL2Segment messages
	LogStartingCreateL2Segment               = "Starting CreateL2Segment"
	LogConstructedCreateL2SegmentRequestBody = "Constructed Create GPCN L2 Segment request body successfully"
	LogConstructedCreateL2SegmentRequest     = "Constructed Create GPCN L2 Segment request successfully"
	LogIssuedCreateL2SegmentJob              = "Successfully issued a job to create GPCN L2 Segment. Beginning long-polling to check the status"
	LogLongPollingCompletedCreateL2Segment   = "Long polling completed for Create GPCN L2 Segment - proceeding to GetL2Segment"
	LogSuccessfullyRetrievedL2SegmentCreate  = "Successfully retrieved GPCN L2 Segment - Create"

	// GetL2Segment messages
	LogStartingGetL2SegmentWithID           = "Starting GetL2Segment for L2 segment ID %s"
	LogSuccessfullyRetrievedL2SegmentWithID = "Successfully retrieved L2 segment with ID %s"

	// UpdateL2Segment messages
	LogStartingUpdateL2SegmentWithID         = "Starting UpdateL2Segment for L2 segment ID %s"
	LogConstructedUpdateL2SegmentRequestBody = "Constructed Update GPCN L2 Segment request body successfully"
	LogConstructedUpdateL2SegmentRequest     = "Constructed Update GPCN L2 Segment request successfully"
	LogNothingToUpdateOnL2Segment            = "No name or description change for L2 segment ID %s. Reading the segment instead"
	LogSuccessfullyRetrievedL2SegmentUpdate  = "Successfully retrieved GPCN L2 Segment - Update"

	// DeleteL2Segment messages
	LogStartingDeleteL2SegmentWithID              = "Starting DeleteL2Segment for L2 segment ID %s"
	LogConstructedDeleteL2SegmentRequest          = "Constructed Delete GPCN L2 Segment request successfully"
	LogIssuedDeleteL2SegmentJob                   = "Successfully issued a job to delete GPCN L2 Segment. Beginning long-polling to check the status"
	LogSuccessfullyCompletedDeleteL2SegmentWithID = "Successfully completed DeleteL2Segment for L2 segment ID %s"

	// Resource-level CRUD operation messages
	LogStartingCreateGPCNL2Segment             = "Starting Create GPCN L2 Segment"
	LogSuccessfullyFinishedCreateGPCNL2Segment = "Successfully finished Create GPCN L2 Segment"
	LogStartingReadGPCNL2Segment               = "Starting Read GPCN L2 Segment"
	LogSuccessfullyRetrievedGPCNL2SegmentRead  = "Successfully retrieved GPCN L2 Segment - Read"
	LogSuccessfullyFinishedReadGPCNL2Segment   = "Successfully finished Read GPCN L2 Segment"
	LogStartingUpdateGPCNL2Segment             = "Starting Update GPCN L2 Segment"
	LogSuccessfullyFinishedUpdateGPCNL2Segment = "Successfully finished Update GPCN L2 Segment"
	LogStartingDeleteGPCNL2Segment             = "Starting Delete GPCN L2 Segment"
	LogSuccessfullyFinishedDeleteGPCNL2Segment = "Successfully finished Delete GPCN L2 Segment"
)

// Drift-detection messages for segments deleted outside of Terraform
const (
	LogL2SegmentNotFoundRemovingFromState = "GPCN L2 Segment no longer exists (deleted outside of Terraform) - removing it from state"
	LogL2SegmentAlreadyDeleted            = "GPCN L2 Segment was already deleted"
)
