package virtualmachines

var BASE_URL string = "/resource/virtual-machines/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300

// Virtual Machine lifecycle statuses
const (
	Running string = "Running"
	Shutoff string = "Shutoff"
)
