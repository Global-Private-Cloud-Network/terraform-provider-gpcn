package volumes

var BASE_URL_V1 string = "/v1/resource/volumes/"
var DATA_CENTERS_BASE_URL_V1 string = "/v1/resource/data-centers/"
var volumeTypeMapping = map[string]string{
	"SSD":  "vol-add-ssd",
	"NVMe": "vol-add-nvme",
}
