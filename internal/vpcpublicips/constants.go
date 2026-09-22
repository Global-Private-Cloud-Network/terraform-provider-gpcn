package vpcpublicips

var BASE_URL_V1 string = "/v1/resource/vpcs/"

// VPC public IP states
const (
	PUBLIC_IP_STATE_ACQUIRING = "acquiring"
	PUBLIC_IP_STATE_READY     = "ready"
	PUBLIC_IP_STATE_FAILED    = "failed"
	PUBLIC_IP_STATE_REMOVING  = "removing"
)

// The API has no single-read route for an address, so a read walks the parent
// listing. The page cap bounds a listing whose paging never reports a last page.
const (
	PUBLIC_IP_LIST_PAGE_SIZE = 100
	PUBLIC_IP_LIST_MAX_PAGES = 100
)

// Job action names. Each one names the operation in a polling diagnostic.
const (
	ActionAcquirePublicIp = "Acquire GPCN VPC Public IP"
	ActionAttachPublicIp  = "Attach GPCN VPC Public IP"
	ActionDetachPublicIp  = "Detach GPCN VPC Public IP"
	ActionReleasePublicIp = "Release GPCN VPC Public IP"
)

// publicIpsURL returns the collection route for one VPC's addresses.
func publicIpsURL(vpcID string) string {
	return BASE_URL_V1 + vpcID + "/public-ips"
}

// publicIpURL returns the route of one address inside its VPC.
func publicIpURL(vpcID, publicIpID string) string {
	return publicIpsURL(vpcID) + "/" + publicIpID
}
