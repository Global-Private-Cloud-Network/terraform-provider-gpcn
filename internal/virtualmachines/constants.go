package virtualmachines

import "time"

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300
var DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS int = 300
var DEFAULT_INITIAL_POLL_DELAY_SECONDS int = 30

// The tests shorten this interval, so it is a variable and not a constant.
var VM_STATUS_POLL_INTERVAL = 5 * time.Second

// The poller waits this long after a status match, so a lagging API cannot mislead it.
var VM_STATUS_SETTLE_WAIT = 5 * time.Second

// VMStatus represents the lifecycle status of a virtual machine
type VMStatus string

// Virtual Machine lifecycle statuses
const (
	VMStatusRunning      VMStatus = "Running"
	VMStatusStopped      VMStatus = "Stopped"
	VMStatusProvisioning VMStatus = "Provisioning"
	VMStatusResizing     VMStatus = "Resizing"
	VMStatusStarting     VMStatus = "Starting"
	VMStatusStopping     VMStatus = "Stopping"
	VMStatusDeleting     VMStatus = "Deleting"
	VMStatusDestroyed    VMStatus = "Destroyed"
	VMStatusShutoff      VMStatus = "Shutoff"
	VMStatusRescue       VMStatus = "Rescue"
	VMStatusRescuing     VMStatus = "Rescuing"
	VMStatusUnrescuing   VMStatus = "Unrescuing"
	VMStatusUnknown      VMStatus = "Unknown"
	VMStatusError        VMStatus = "Error"
)

// A virtual machine in one of these statuses never reaches a different one.
// The platform writes both of them from its own delete path.
// The poller stops immediately instead of waiting for the timeout.
// Error is absent, because the provider mapper returns it for every status it does not know.
// Unknown is absent, because that status can be transient.
var vmTerminalFailureStatuses = []VMStatus{VMStatusDestroyed, VMStatusDeleting}

// String returns the string representation of VMStatus
func (s VMStatus) String() string {
	return string(s)
}
