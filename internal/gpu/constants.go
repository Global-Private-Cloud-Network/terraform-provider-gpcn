package gpu

var BASE_URL_V1 string = "/v1/resource/gpu/"

// Supported GPU Series Names
var GPUSeriesNames = []string{"H100 Series", "RTX A6000 Series", "RTX PRO 6000 Blackwell Series", "A100 Series"}

// Supported GPU Series Codes
var GPUSeriesCodes = []string{"h100_series", "rtx_a6000_series", "rtx_pro_6000_blackwell_series", "a100_series"}

// GPUSeriesNameToCode maps GPU series names to their corresponding codes
var GPUSeriesNameToCode = map[string]string{
	"H100 Series":                   "h100_series",
	"RTX A6000 Series":              "rtx_a6000_series",
	"RTX PRO 6000 Blackwell Series": "rtx_pro_6000_blackwell_series",
	"A100 Series":                   "a100_series",
}
