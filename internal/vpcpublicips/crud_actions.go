package vpcpublicips

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// PublicIp is the API's only projection of an address. There is no detail
// route, so every read of one row comes out of the parent listing.
type PublicIp struct {
	Id                 string  `json:"id"`
	IpAddress          *string `json:"ipAddress"`
	State              string  `json:"state"`
	FailureReason      *string `json:"failureReason"`
	VirtualMachineId   *string `json:"virtualMachineId"`
	VirtualMachineName *string `json:"virtualMachineName"`
	ActiveJobId        *string `json:"activeJobId"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`
}

// acquirePublicIpResponse names the sibling the acquire 202 carries beside the
// job id. The API inserts the row before it dispatches the job.
type acquirePublicIpResponse struct {
	Data struct {
		PublicIpID string `json:"publicIpId"`
	} `json:"data"`
}

type listPublicIpsResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    []PublicIp `json:"data"`
	Meta    struct {
		Page        int  `json:"page"`
		TotalPages  int  `json:"totalPages"`
		HasNextPage bool `json:"hasNextPage"`
	} `json:"meta"`
}

// AcquirePublicIp asks the VPC for a new elastic address and returns its id.
// The address is a holding until an attach binds it to a network interface.
func AcquirePublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (string, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAcquirePublicIp, vpcID))

	// The acquire route takes no payload. Its schema is strict, so any key is a 422.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, publicIpsURL(vpcID), nil)
	if err != nil {
		return "", err
	}

	jobID, body, err := issueJob(gpcnClient, ctx, request, LogIssuedAcquirePublicIpJob)
	if err != nil {
		return "", err
	}

	var acquireResponse acquirePublicIpResponse
	if err := json.Unmarshal(body, &acquireResponse); err != nil {
		return "", err
	}

	// The API inserts the row before it dispatches the job, so a failed job
	// still leaves a real address. The caller records the id and releases it.
	if err := pollJob(gpcnClient, ctx, ActionAcquirePublicIp, jobID); err != nil {
		return acquireResponse.Data.PublicIpID, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyAcquiredPublicIp, acquireResponse.Data.PublicIpID))
	return acquireResponse.Data.PublicIpID, nil
}

// GetPublicIp returns one address out of its VPC's listing. An address that no
// page names answers a 404, so callers read it like any other not-found.
func GetPublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, publicIpID string) (*PublicIp, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetPublicIp, vpcID, publicIpID))

	for page := 1; page <= PUBLIC_IP_LIST_MAX_PAGES; page++ {
		listResponse, err := listPublicIps(gpcnClient, ctx, vpcID, page)
		if err != nil {
			return nil, err
		}

		for i := range listResponse.Data {
			if listResponse.Data[i].Id == publicIpID {
				tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedPublicIp, publicIpID))
				return &listResponse.Data[i], nil
			}
		}

		// Only a listing that reported its last page proves the address is
		// gone. Stopping at the cap says nothing, and a not-found there would
		// drop a live address out of state.
		if !listResponse.Meta.HasNextPage {
			return nil, &client.HTTPError{StatusCode: http.StatusNotFound, Message: ErrDetailPublicIpNotFound}
		}
	}

	return nil, fmt.Errorf(ErrDetailPublicIpListingTruncated, PUBLIC_IP_LIST_MAX_PAGES, vpcID)
}

func listPublicIps(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, page int) (*listPublicIpsResponse, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(PUBLIC_IP_LIST_PAGE_SIZE))
	query.Set("page", strconv.Itoa(page))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, publicIpsURL(vpcID)+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var listResponse listPublicIpsResponse
	if err := json.Unmarshal(body, &listResponse); err != nil {
		return nil, err
	}

	return &listResponse, nil
}

// AttachPublicIp binds an address to one network interface. The API names the
// interface, never the machine, because a VPC machine can carry interfaces in
// several subnets.
func AttachPublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, publicIpID, nicID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingAttachPublicIp, publicIpID, nicID))

	attachRequestBody, err := json.Marshal(map[string]any{"nicId": nicID})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, publicIpURL(vpcID, publicIpID)+"/attach", bytes.NewBuffer(attachRequestBody))
	if err != nil {
		return err
	}

	jobID, _, err := issueJob(gpcnClient, ctx, request, LogIssuedAttachPublicIpJob)
	if err != nil {
		return err
	}
	if err := pollJob(gpcnClient, ctx, ActionAttachPublicIp, jobID); err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyAttachedPublicIp, publicIpID))
	return nil
}

// DetachPublicIp unbinds an address and leaves it held by the VPC. The
// address attaches again without a new acquisition.
func DetachPublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, publicIpID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDetachPublicIp, publicIpID))

	// The detach route takes no payload, and its schema rejects a body with keys.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, publicIpURL(vpcID, publicIpID)+"/detach", nil)
	if err != nil {
		return err
	}

	jobID, _, err := issueJob(gpcnClient, ctx, request, LogIssuedDetachPublicIpJob)
	if err != nil {
		return err
	}
	if err := pollJob(gpcnClient, ctx, ActionDetachPublicIp, jobID); err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyDetachedPublicIp, publicIpID))
	return nil
}

// ReleasePublicIp gives the address back to the provider. An address another
// operator already released answers 404, which leaves nothing to do.
func ReleasePublicIp(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, publicIpID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingReleasePublicIp, publicIpID))

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, publicIpURL(vpcID, publicIpID), nil)
	if err != nil {
		return err
	}

	// Only the release request itself can report that the address is already
	// gone. A 404 from the job poll is a routing failure, not an answer about
	// the address.
	jobID, _, err := issueJob(gpcnClient, ctx, request, LogIssuedReleasePublicIpJob)
	if client.IsNotFound(err) {
		tflog.Info(ctx, LogPublicIpAlreadyReleased)
		return nil
	}
	if err != nil {
		return err
	}
	if err := pollJob(gpcnClient, ctx, ActionReleasePublicIp, jobID); err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyReleasedPublicIp, publicIpID))
	return nil
}

// issueJob sends a request whose 202 carries a job id. It returns that id with
// the raw body, because the acquire 202 names a sibling field as well.
func issueJob(gpcnClient *client.GpcnClient, ctx context.Context, request *http.Request, issuedMessage string) (string, []byte, error) {
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	tflog.Info(ctx, issuedMessage)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", nil, err
	}

	var jobResponse client.JobStatusSingularResponse
	if err := json.Unmarshal(body, &jobResponse); err != nil {
		return "", nil, err
	}

	return jobResponse.Data.JobID, body, nil
}

func pollJob(gpcnClient *client.GpcnClient, ctx context.Context, action, jobID string) error {
	if _, err := client.PerformLongPolling(gpcnClient, ctx, action, jobID); err != nil {
		return fmt.Errorf("%s polling failed: %w", action, err)
	}
	return nil
}
