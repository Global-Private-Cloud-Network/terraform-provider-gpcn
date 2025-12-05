package volumes

var BASE_URL string = "/resource/volumes/"
var volumeTypeMapping = map[string]int64{
	"SSD":  1,
	"NVMe": 2,
}
