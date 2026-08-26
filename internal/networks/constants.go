package networks

import "time"

var VIRTUAL_MACHINES_BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var BASE_URL_V1 string = "/v1/resource/networks/"

// Network types
var NETWORK_TYPE_CUSTOM = "custom"
var NETWORK_TYPE_STANDARD = "standard"

// Delete Network constants
var DELETE_NETWORK_RETRY_COUNT = 5

// DELETE_NETWORK_RETRY_INTERVAL is the interval between delete attempts. This is
// a var, not a constant, because the tests decrease it.
var DELETE_NETWORK_RETRY_INTERVAL = 5 * time.Second
