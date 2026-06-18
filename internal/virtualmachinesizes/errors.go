package virtualmachinesizes

const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToFetchSizes      = "Unable to fetch virtual machine sizes"

	ErrDetailExpectedGpcnClient = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
)
