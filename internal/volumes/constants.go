package volumes

import "time"

var BASE_URL_V1 string = "/v1/resource/volumes/"
var DATA_CENTERS_BASE_URL_V1 string = "/v1/resource/data-centers/"
var volumeTypeMapping = map[string]string{
	"SSD":  "vol-add-ssd",
	"NVMe": "vol-add-nvme",
}

// BusyRetryInterval is the wait before a request goes out again after the job
// that held the resource ended. It is a variable so that tests can shorten it.
var BusyRetryInterval = 5 * time.Second
