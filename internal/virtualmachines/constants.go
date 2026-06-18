package virtualmachines

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300
var DEFAULT_VIRTUALMACHINE_STATUS_TIMEOUT_SECONDS int = 300
var DEFAULT_INITIAL_POLL_DELAY_SECONDS int = 30

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
