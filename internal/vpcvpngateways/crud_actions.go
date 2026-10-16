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

// ParseAPIError returns the code and the message of an API error. Both are
// empty when err is not an API error.
func ParseAPIError(err error) (code, message string) {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code, httpErr.Message
	}
	return "", ""
}

// ErrorDetail gives the API's message for an API error. The codes collide:
// "Duplicate Resource" names many refusals, and only the message tells which.
func ErrorDetail(err error) string {
	if _, message := ParseAPIError(err); message != "" {
		return message
	}
	return err.Error()
}

// CreateVpnGateway posts the gateway and polls its job. The body is an empty
// object, because the API refuses any key. The 202 row still says "creating", so
// the caller reads the gateway with GetVpnGateway.
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

// GetVpnGateway reads the VPC's gateway. A 404 reads as ErrVpnGatewayNotFound.
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

// DeleteVpnGateway issues the removal and polls its job. The job ending does not
// mean the row is gone, because the platform parks a removal and a sweeper
// retries it. WaitForVpnGatewayGone settles that.
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
// API answers 404, or until the polling timeout. A parked removal keeps the wait
// going.
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

// issueJob sends a request whose 202 carries a job ID. The singular job envelope
// decodes the job ID and ignores the row.
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
