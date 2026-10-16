package vpcvpngateways

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ApiVpnGateway is the VpnGateway projection every gateway route answers with.
// Every field is on the wire. The nullable ones are pointers.
type ApiVpnGateway struct {
	ID            string  `json:"id"`
	VpcID         string  `json:"vpcId"`
	Kind          string  `json:"kind"`
	State         string  `json:"state"`
	Address       *string `json:"address"`
	ReservedBlock *string `json:"reservedBlock"`
	FailureReason *string `json:"failureReason"`
	ActiveJobID   *string `json:"activeJobId"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

type getVpnGatewayResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    ApiVpnGateway `json:"data"`
}

func gatewayPath(vpcID string) string {
	return BaseURLV1 + vpcID + gatewayPathSegment
}

// ParseAPIError returns the code and the message of an API error response. The
// client parses the envelope where it reads the body. The message is the
// sentence the API wrote, byte for byte. Both are empty when err is not an API
// error, or when its body held no envelope.
func ParseAPIError(err error) (code, message string) {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code, httpErr.Message
	}
	return "", ""
}

// ErrorDetail is the text a diagnostic carries for err. An API refusal reads as
// the API's own message, because its codes collide ("Duplicate Resource" names
// a dozen different refusals) and the sentence is what tells the operator what
// to do. Any other error reads as itself.
func ErrorDetail(err error) string {
	if _, message := ParseAPIError(err); message != "" {
		return message
	}
	return err.Error()
}

// CreateVpnGateway posts the gateway and polls the job that builds it. The body
// is an empty object, because the API refuses any key. The 202 answers with the
// job ID and a row that still says "creating", so the caller reads the gateway
// back with GetVpnGateway.
func CreateVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingCreateVpnGateway, vpcID))

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayPath(vpcID), bytes.NewBufferString("{}"))
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedCreateVpnGatewayJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateVpnGateway, jobID); err != nil {
		return fmt.Errorf(ErrFmtCreatePollingFailed, err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVpnGateway)
	return nil
}

// GetVpnGateway reads the VPC's gateway. A 404 reads as ErrVpnGatewayNotFound,
// and the API error stays in the chain.
func GetVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (*ApiVpnGateway, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVpnGateway, vpcID))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayPath(vpcID), nil)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		if client.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %w", ErrVpnGatewayNotFound, err)
		}
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var gateway getVpnGatewayResponse
	if err := json.Unmarshal(body, &gateway); err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVpnGateway, vpcID))
	return &gateway.Data, nil
}

// DeleteVpnGateway issues the removal and polls its job. The job ending does
// not mean the row is gone: a removal the platform parks is retried by a
// sweeper. WaitForVpnGatewayGone settles that. A 409 refusal reaches the caller
// as the API error, so ErrorDetail gives its message.
func DeleteVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpnGateway, vpcID))

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, gatewayPath(vpcID), nil)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteVpnGatewayJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteVpnGateway, jobID); err != nil {
		return fmt.Errorf(ErrFmtDeletePollingFailed, err)
	}
	return nil
}

// WaitForVpnGatewayGone reads the gateway every RemovalPollInterval until the
// API answers 404. It stops at the client's polling timeout. A gateway in the
// removing state with a failure reason is a parked removal, and the sweeper
// retries it, so the wait goes on.
func WaitForVpnGatewayGone(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	timeout := client.DefaultPollingConfig().Timeout
	if config := gpcnClient.Config(); config != nil && config.PollingTimeout > 0 {
		timeout = config.PollingTimeout
	}
	return waitForVpnGatewayGone(gpcnClient, ctx, vpcID, RemovalPollInterval, timeout)
}

func waitForVpnGatewayGone(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, interval, timeout time.Duration) error {
	tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpnGatewayGone, vpcID))

	startTime := time.Now()
	var lastState, lastReason string

	for {
		gateway, err := GetVpnGateway(gpcnClient, ctx, vpcID)
		if errors.Is(err, ErrVpnGatewayNotFound) {
			tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyDeletedVpnGW, vpcID))
			return nil
		}
		if err != nil {
			return fmt.Errorf(ErrFmtWaitForRemovalFailed, err)
		}

		lastState = gateway.State
		lastReason = removalTimeoutNoReason
		if gateway.FailureReason != nil && *gateway.FailureReason != "" {
			lastReason = *gateway.FailureReason
		}
		tflog.Info(ctx, fmt.Sprintf(LogVpnGatewayStillRemoving, vpcID, lastState))

		if time.Since(startTime) >= timeout {
			return fmt.Errorf("%w: "+ErrFmtRemovalTimeout, ErrRemovalTimeout, vpcID, lastState, timeout, lastReason)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// issueJob sends a request whose 202 carries a job ID. The gateway routes
// answer data:{jobId, vpnGateway}, and the singular job envelope decodes the
// job ID and leaves the row. The request already carries the context, so this
// helper takes none.
func issueJob(gpcnClient *client.GpcnClient, request *http.Request) (string, error) {
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	var accepted client.JobStatusSingularResponse
	if err := json.Unmarshal(body, &accepted); err != nil {
		return "", err
	}
	if accepted.Data.JobID == "" {
		return "", errors.New(ErrDetailNoJobIDInResponse)
	}
	return accepted.Data.JobID, nil
}
