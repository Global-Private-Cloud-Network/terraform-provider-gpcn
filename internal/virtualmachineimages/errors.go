package virtualmachineimages

const (
	ErrSummaryUnexpectedConfigureType = "Unexpected Data Source Configure Type"
	ErrSummaryUnableToFetchImages     = "Unable to fetch virtual machine images"

	ErrDetailExpectedGpcnClient    = "Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers."
	ErrDetailImageNameNotFound     = "No image matching %q was found for this datacenter. Available images: %s"
	ErrDetailNoImagesForDatacenter = "No virtual machine images are available for this datacenter"
)
