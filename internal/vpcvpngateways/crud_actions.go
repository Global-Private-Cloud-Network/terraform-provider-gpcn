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

// RowMayExist reports whether a POST that failed with err can have created a
// row. A 4xx answer is a refusal. A 5xx answer or a transport error leaves the
// outcome unknown.
func RowMayExist(err error) bool {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= http.StatusInternalServerError
	}
	return true
}

// CreateVpnGateway posts the gateway once and polls its job. The body is an
// empty object, because the API refuses any key. The 202 row still says
// "creating", so the caller reads the gateway with GetVpnGateway. The first
// result is true when a row may exist, even if the error is not nil. The second
// result is true when the API accepted the POST with a 2xx answer.
func CreateVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (bool, bool, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingCreateVpnGateway, vpcID))

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayPath(vpcID), bytes.NewBufferString("{}"))
	if err != nil {
		return false, false, err
	}

	jobID, err := issueJob(gpcnClient.DoOnce, request)
	if err != nil {
		return RowMayExist(err), false, err
	}
	tflog.Info(ctx, LogIssuedCreateVpnGatewayJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateVpnGateway, jobID); err != nil {
		return true, true, fmt.Errorf(ErrFmtCreatePollingFailed, err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateVpnGateway)
	return true, true, nil
}

// ReadAfterFailure reads the gateway after a create failed. A canceled context
// would fail the read, so it runs on a fresh context bounded by the request
// timeout.
func ReadAfterFailure(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) (*ApiVpnGateway, error) {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), gpcnClient.Config().RequestTimeout)
		defer cancel()
	}
	return GetVpnGateway(gpcnClient, ctx, vpcID)
}

// CreatedSince reports whether a row with this creation time can come from a
// request sent at requestStart. A time that does not parse never qualifies.
func CreatedSince(createdAt string, requestStart time.Time) bool {
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	return err == nil && !created.Before(requestStart.Add(-AdoptWindow))
}

// PollAdoptedJob waits for the job that an adopted row still runs. It shares
// one deadline with the request: the polling timeout, counted from
// requestStart. A failure is only logged, because the caller reads the row next.
func PollAdoptedJob(gpcnClient *client.GpcnClient, ctx context.Context, action string, activeJobID *string, requestStart time.Time) {
	if activeJobID == nil || *activeJobID == "" {
		return
	}
	pollConfig := client.DefaultPollingConfig()
	if config := gpcnClient.Config(); config != nil && config.PollingTimeout > 0 {
		pollConfig.Timeout = config.PollingTimeout
	}
	pollConfig.Timeout = max(pollConfig.Timeout-time.Since(requestStart), 0)
	if _, err := client.PerformLongPollingWithConfig(gpcnClient, ctx, action, *activeJobID, pollConfig); err != nil {
		tflog.Info(ctx, LogAdoptedJobPollFailed)
	}
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

// DeleteVpnGateway issues the removal, polls its job, and waits for the 404. The
// job ending does not mean the row is gone, because the platform parks a removal
// and a sweeper retries it. A job that failed and a 409 VPN_GATEWAY_REMOVING both
// mean a removal is under way, so both go on to the wait. A 409
// VPN_GATEWAY_BUSY_CREATING waits for the create to end and sends the removal
// again. The job poll and the waits share one deadline, the polling timeout.
func DeleteVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string) error {
	timeout := client.DefaultPollingConfig().Timeout
	if config := gpcnClient.Config(); config != nil && config.PollingTimeout > 0 {
		timeout = config.PollingTimeout
	}
	return deleteVpnGateway(gpcnClient, ctx, vpcID, RemovalPollInterval, timeout)
}

func deleteVpnGateway(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, interval, timeout time.Duration) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpnGateway, vpcID))
	startTime := time.Now()

	for {
		if time.Since(startTime) >= timeout {
			return fmt.Errorf(ErrFmtStillCreating, vpcID, timeout)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, gatewayPath(vpcID), nil)
		if err != nil {
			return err
		}

		jobID, err := issueJob(gpcnClient.DoWithRetry, request)
		code, _ := ParseAPIError(err)
		if code == CodeGatewayBusyCreating {
			tflog.Info(ctx, LogDeleteWaitsForCreate)
			gone, waitErr := waitForVpnGatewayCreated(gpcnClient, ctx, vpcID, interval, startTime, timeout)
			if waitErr != nil {
				return waitErr
			}
			if gone {
				return nil
			}
			continue
		}
		if code == CodeGatewayRemoving {
			tflog.Info(ctx, LogRemovalAlreadyRunning)
		} else if err != nil {
			return err
		} else {
			tflog.Info(ctx, LogIssuedDeleteVpnGatewayJob)
			pollConfig := client.DefaultPollingConfig()
			pollConfig.Timeout = max(timeout-time.Since(startTime), 0)
			if _, err := client.PerformLongPollingWithConfig(gpcnClient, ctx, ActionDeleteVpnGateway, jobID, pollConfig); err != nil {
				if !errors.Is(err, client.ErrJobFailed) {
					return fmt.Errorf(ErrFmtDeletePollingFailed, err)
				}
				tflog.Info(ctx, LogDeleteJobFailedWaiting)
			}
		}

		return waitForVpnGatewayGone(gpcnClient, ctx, vpcID, interval, startTime, timeout)
	}
}

// waitForVpnGatewayCreated reads the gateway every interval until it leaves
// the creating state, or until the timeout counted from startTime. The first
// result is true when the API answers 404, which means no removal is left to
// send.
func waitForVpnGatewayCreated(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, interval time.Duration, startTime time.Time, timeout time.Duration) (bool, error) {
	for {
		gateway, err := GetVpnGateway(gpcnClient, ctx, vpcID)
		if errors.Is(err, ErrVpnGatewayNotFound) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf(ErrFmtWaitForCreateFailed, err)
		}
		if gateway.State != StateCreating {
			return false, nil
		}

		if time.Since(startTime) >= timeout {
			return false, fmt.Errorf(ErrFmtStillCreating, vpcID, timeout)
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// waitForVpnGatewayGone reads the gateway every interval until the API answers
// 404, or until the timeout counted from startTime. A parked removal keeps the
// wait going.
func waitForVpnGatewayGone(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, interval time.Duration, startTime time.Time, timeout time.Duration) error {
	tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpnGatewayGone, vpcID))

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
func issueJob(send func(*http.Request) (*http.Response, error), request *http.Request) (string, error) {
	response, err := send(request)
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
