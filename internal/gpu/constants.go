package gpu

var BASE_URL_V1 string = "/v1/resource/gpu/"

// The six advertised GPU series. The inventory response, not this list, decides
// what a datacenter offers.
var GPUSeriesNames = []string{"NVIDIA H200 Series", "NVIDIA H100 Series", "NVIDIA A100 Series", "NVIDIA RTX PRO 6000 Blackwell", "NVIDIA RTX A6000 Series", "NVIDIA L40 Series"}

// The catalog code of each advertised series, in the order of GPUSeriesNames.
var GPUSeriesCodes = []string{"nvidia-h200-series", "nvidia-h100-series", "nvidia-a100-series", "nvidia-rtx_pro_6000-series", "nvidia-rtx_a6000-series", "nvidia-l40-series"}

// GPUSeriesNameToCode answers only when the inventory response lists no series.
// The catalog is the authority.
var GPUSeriesNameToCode = map[string]string{
	"NVIDIA H200 Series":            "nvidia-h200-series",
	"NVIDIA H100 Series":            "nvidia-h100-series",
	"NVIDIA A100 Series":            "nvidia-a100-series",
	"NVIDIA RTX PRO 6000 Blackwell": "nvidia-rtx_pro_6000-series",
	"NVIDIA RTX A6000 Series":       "nvidia-rtx_a6000-series",
	"NVIDIA L40 Series":             "nvidia-l40-series",
}

// Supported GPU Image Names
var GPUImageNames = []string{"ubuntu-22.04", "ubuntu-24.04"}
