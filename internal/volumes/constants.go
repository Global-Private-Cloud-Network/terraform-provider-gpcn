package volumes

import (
	"maps"
	"regexp"
	"slices"
)

var BASE_URL_V1 string = "/v1/resource/volumes/"
var DATA_CENTERS_BASE_URL_V1 string = "/v1/resource/data-centers/"
var volumeTypeMapping = map[string]string{
	"SSD":  "vol-add-ssd",
	"NVMe": "vol-add-nvme",
}

// VolumeTypeCodePattern matches a storage component code. The datacenter decides
// which codes it offers, so a new storage class must not need a provider release.
var VolumeTypeCodePattern = regexp.MustCompile(`^vol-add-[a-z0-9-]+$`)

// VolumeTypeAliases returns the display names the schema accepts beside a code.
func VolumeTypeAliases() []string {
	return slices.Sorted(maps.Keys(volumeTypeMapping))
}
