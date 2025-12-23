package virtualmachines

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300

// Virtual Machine lifecycle statuses
const (
	Running string = "Running"
	Shutoff string = "Shutoff"
)
