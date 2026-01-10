package gpu

// Error summary constants
const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Configure Type"
	ErrSummaryUnableToCreateGPU       = "Unable to Create GPU"
	ErrSummaryUnableToReadGPU         = "Unable to Read GPU"
	ErrSummaryUnableToUpdateGPU       = "Unable to Update GPU"
	ErrSummaryUnableToDeleteGPU       = "Unable to Delete GPU"
	ErrSummaryUnableToCreateRequest   = "Unable to Create Request"
	ErrSummaryErrorReadingResponse    = "Error Reading Response Body"
	ErrSummaryErrorUnmarshalingJSON   = "Error Unmarshaling JSON"
)

// Error detail message templates
const (
	ErrDetailExpectedHTTPClient                   = "Expected *http.Client, got: %T. Please report this issue to the provider developers."
	ErrDetailCreateGPUFailed                      = "Failed to create GPU"
	ErrDetailReadGPUFailed                        = "Failed to read GPU with ID %s"
	ErrDetailUpdateGPUFailed                      = "Failed to update GPU with ID %s"
	ErrDetailDeleteGPUFailed                      = "Failed to delete GPU with ID %s"
	ErrDetailUnableToCreateRequest                = "Unable to create HTTP request"
	ErrDetailUnableToReadResponseBody             = "Unable to read response body"
	ErrDetailUnableToUnmarshalJSON                = "Unable to unmarshal JSON response"
	ErrDetailMalformedResponseMissingData         = "Malformed inventory response: missing Data"
	ErrDetailMalformedResponseMissingAvailability = "Malformed inventory response: missing Availability for datacenter"
	ErrDetailMalformedResponseMissingGPUCounts    = "Malformed inventory response: missing GPUCounts"
	ErrDetailNoInventoryAvailable                 = "No GPU availability for series code %s in datacenter %s with GPU count %d"
)

// Error summary constants for inventory
const (
	ErrSummaryUnableToCheckInventory     = "Unable to Check GPU Inventory"
	ErrSummaryMalformedInventoryResponse = "Malformed Inventory Response"
	ErrSummaryNoInventoryAvailable       = "No GPU Inventory Available"
)
