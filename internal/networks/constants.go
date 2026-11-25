package networks

import "time"

var VIRTUAL_MACHINES_BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var BASE_URL_V1 string = "/v1/resource/networks/"

// Network types
var NETWORK_TYPE_CUSTOM = "custom"
var NETWORK_TYPE_STANDARD = "standard"

// Delete Network constants
var DELETE_NETWORK_RETRY_COUNT = 5

// BusyRetryInterval is the wait before a request goes out again after the job
// that held the resource ended. It is a variable so that tests can shorten it.
var BusyRetryInterval = 5 * time.Second
