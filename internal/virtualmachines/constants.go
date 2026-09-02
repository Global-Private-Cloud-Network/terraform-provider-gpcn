package virtualmachines

import "time"

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT = 5 * time.Minute
var DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT = 5 * time.Minute
var DEFAULT_INITIAL_POLL_DELAY = 30 * time.Second

// VIRTUALMACHINE_STATUS_POLL_INTERVAL is the interval between status checks. It
// is also the delay before the poll confirms a target status.
//
// This is a var only so that a test can decrease it, in this package and in
// internal/volumeattachments, which reaches the poll through AttachVolume.
// Production code must not assign it.
var VIRTUALMACHINE_STATUS_POLL_INTERVAL = 5 * time.Second

// VMStatus represents the lifecycle status of a virtual machine
type VMStatus string

// Virtual Machine lifecycle statuses
const (
	VMStatusRunning VMStatus = "Running"
	VMStatusShutoff VMStatus = "Shutoff"
)

// String returns the string representation of VMStatus
func (s VMStatus) String() string {
	return string(s)
}
