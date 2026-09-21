package l2segments

// BASE_URL_V1 is the segment collection. The create and the list are mounted on
// the trailing slash, and every per-segment route appends the segment ID.
var BASE_URL_V1 string = "/v1/resource/l2-segments/"

// ErrorCodeL2SegmentInUse is the only typed code a delete refusal carries. Every
// other refusal on the route carries the generic conflict code, so the provider
// must not branch on a code to identify them.
const ErrorCodeL2SegmentInUse = "L2_SEGMENT_IN_USE"

// DetailKeyAttachedNicCount names the count the in-use refusal carries.
const DetailKeyAttachedNicCount = "attachedNicCount"

// Polling action names. Each one reaches the user inside a stopped-job error.
const (
	ActionCreateL2Segment = "Create GPCN L2 Segment"
	ActionDeleteL2Segment = "Delete GPCN L2 Segment"
)

// StateFailed is the state a parked segment carries. The row stays live and
// tenant-visible, so Read keeps it in state rather than removing it.
const StateFailed = "failed"
