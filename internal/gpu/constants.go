package gpu

var BASE_URL_V1 string = "/v1/resource/gpu/"

// Supported GPU Series Names
var GPUSeriesNames = []string{"NVIDIA H200 Series", "NVIDIA H100 Series", "NVIDIA A100 Series", "NVIDIA RTX PRO 6000 Blackwell", "NVIDIA RTX A6000 Series", "NVIDIA L40 Series"}

// Supported GPU Series Codes
var GPUSeriesCodes = []string{"nvidia-h200-series", "nvidia-h100_series", "nvidia-a100_series", "nvidia-rtx_pro_6000-series", "nvidia-rtx_a6000-series", "nvidia-l40-series"}

// GPUSeriesNameToCode maps GPU series names to their corresponding codes
var GPUSeriesNameToCode = map[string]string{
	"NVIDIA H200 Series":            "nvidia-h200-series",
	"NVIDIA H100 Series":            "nvidia-h100_series",
	"NVIDIA A100 Series":            "nvidia-a100_series",
	"NVIDIA RTX PRO 6000 Blackwell": "nvidia-rtx_pro_6000-series",
	"NVIDIA RTX A6000 Series":       "nvidia-rtx_a6000-series",
	"NVIDIA L40 Series":             "nvidia-l40-series",
}

// Supported GPU Image Names
var GPUImageNames = []string{"ubuntu-22.04", "ubuntu-24.04"}
