package vpcvpnconnections

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
	"terraform-provider-gpcn/internal/vpcvpngateways"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ApiVpnConnection is the VpnConnection projection. The nullable fields are
// pointers.
type ApiVpnConnection struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Description          *string  `json:"description"`
	State                string   `json:"state"`
	ProviderState        *string  `json:"providerState"`
	FailureReason        *string  `json:"failureReason"`
	GatewayKind          string   `json:"gatewayKind"`
	LocalEndpointAddress *string  `json:"localEndpointAddress"`
	PeerAddress          string   `json:"peerAddress"`
	PeerCidrs            []string `json:"peerCidrs"`
	IkeVersion           string   `json:"ikeVersion"`
	Encryption           string   `json:"encryption"`
	Integrity            string   `json:"integrity"`
	DhGroup              int64    `json:"dhGroup"`
	PfsGroup             int64    `json:"pfsGroup"`
	IkeLifetimeSeconds   int64    `json:"ikeLifetimeSeconds"`
	IpsecLifetimeSeconds int64    `json:"ipsecLifetimeSeconds"`
	Dpd                  bool     `json:"dpd"`
	DesiredApplied       bool     `json:"desiredApplied"`
	ActiveJobID          *string  `json:"activeJobId"`
	AcceptedVerbs        []string `json:"acceptedVerbs"`
	CreatedAt            string   `json:"createdAt"`
	UpdatedAt            string   `json:"updatedAt"`
}

// ApiParameterSheet is the part of the detail's parameter sheet that the
// provider reads. The other keys repeat the connection.
type ApiParameterSheet struct {
	LocalNetworks []string `json:"localNetworks"`
}

// ApiVpnConnectionDetail is the answer of the detail route.
type ApiVpnConnectionDetail struct {
	ApiVpnConnection
	ParameterSheet ApiParameterSheet `json:"parameterSheet"`
}

type getVpnConnectionResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    ApiVpnConnectionDetail `json:"data"`
}

type createVpnConnectionResponse struct {
	Data struct {
		JobID         string `json:"jobId"`
		VpnConnection struct {
			ID string `json:"id"`
		} `json:"vpnConnection"`
	} `json:"data"`
}

func collectionPath(vpcID string) string {
	return BaseURLV1 + vpcID + connectionsPathSegment
}

func connectionPath(vpcID, connectionID string) string {
	return collectionPath(vpcID) + "/" + connectionID
}

func marshalBody(body map[string]any) (*bytes.Reader, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(encoded), nil
}

// CreateVpnConnection posts the VPN connection once and polls its job. It
// returns the connection ID as soon as the API accepts the POST, even when the
// job then fails, so the caller can read the row the job left. The ID is empty
// when the answer carried none. The second result is true when a row may exist
// whatever the ID says: a 5xx answer or a transport error hides the outcome of
// the POST, and the caller then looks for the row by name.
func CreateVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, body map[string]any) (string, bool, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingCreateVpnConnection, vpcID))

	reader, err := marshalBody(body)
	if err != nil {
		return "", false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, collectionPath(vpcID), reader)
	if err != nil {
		return "", false, err
	}

	raw, err := sendAccepted(gpcnClient.DoOnce, request)
	if err != nil {
		return "", vpcvpngateways.RowMayExist(err), err
	}
	var accepted createVpnConnectionResponse
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return "", true, err
	}
	connectionID := accepted.Data.VpnConnection.ID
	if accepted.Data.JobID == "" {
		return connectionID, true, errors.New(ErrDetailNoJobIDInResponse)
	}
	if connectionID == "" {
		return "", true, errors.New(ErrDetailNoConnectionID)
	}
	tflog.Info(ctx, LogIssuedCreateVpnConnectionJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateVpnConnection, accepted.Data.JobID); err != nil {
		return connectionID, true, fmt.Errorf(ErrFmtCreatePollingFailed, err)
	}
	return connectionID, true, nil
}

type listVpnConnectionsResponse struct {
	Data []ApiVpnConnection `json:"data"`
	Meta struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"meta"`
}

// FindVpnConnectionByName pages through the connections of the VPC and returns
// the one with the given name, or nil. Names are unique within a VPC.
func FindVpnConnectionByName(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, name string) (*ApiVpnConnection, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingFindVpnConnection, vpcID))

	for page := 1; page <= listMaxPages; page++ {
		target := fmt.Sprintf("%s?page=%d&limit=%d", collectionPath(vpcID), page, listPageSize)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		raw, err := sendAccepted(gpcnClient.DoWithRetry, request)
		if err != nil {
			return nil, err
		}
		var parsed listVpnConnectionsResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		for index := range parsed.Data {
			if parsed.Data[index].Name == name {
				return &parsed.Data[index], nil
			}
		}
		if !parsed.Meta.HasNextPage {
			return nil, nil
		}
	}
	return nil, nil
}

// afterFailureContext returns a context that a canceled apply does not stop.
// A lookup after a failed create must still run, and the request timeout bounds
// it.
func afterFailureContext(gpcnClient *client.GpcnClient, ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), gpcnClient.Config().RequestTimeout)
}

// FindAfterFailure looks for the connection by name after a create failed.
func FindAfterFailure(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, name string) (*ApiVpnConnection, error) {
	ctx, cancel := afterFailureContext(gpcnClient, ctx)
	defer cancel()
	return FindVpnConnectionByName(gpcnClient, ctx, vpcID, name)
}

// ReadAfterFailure reads the connection after a create failed.
func ReadAfterFailure(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string) (*ApiVpnConnectionDetail, error) {
	ctx, cancel := afterFailureContext(gpcnClient, ctx)
	defer cancel()
	return GetVpnConnection(gpcnClient, ctx, vpcID, connectionID)
}

// MadeByRequest reports whether a listed row can come from a create request
// that was sent at requestStart with the given name and peer address.
func MadeByRequest(row *ApiVpnConnection, name, peerAddress string, requestStart time.Time) bool {
	return row.Name == name && row.PeerAddress == peerAddress && vpcvpngateways.CreatedSince(row.CreatedAt, requestStart)
}

// RetryUnappliedDue reports whether the plan sends the saved settings again.
// That holds when the data center does not run them, the connection is neither
// being created nor removed, and no job runs.
func RetryUnappliedDue(desiredApplied bool, state string, jobActive bool) bool {
	busy := state == StateCreating || state == StateRemoving
	return !desiredApplied && !busy && !jobActive
}

// GetVpnConnection reads the detail of a VPN connection. A 404 reads as
// ErrVpnConnectionNotFound, and the API error stays in the chain.
func GetVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string) (*ApiVpnConnectionDetail, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetVpnConnection, vpcID, connectionID))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connectionPath(vpcID, connectionID), nil)
	if err != nil {
		return nil, err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		if client.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %w", ErrVpnConnectionNotFound, err)
		}
		return nil, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var parsed getVpnConnectionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedVpnConnection, connectionID))
	return &parsed.Data, nil
}

// UpdateVpnConnection puts the changed keys and polls the job. The API saves
// the values before the job runs, so a failed job keeps them.
func UpdateVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string, body map[string]any) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateVpnConnection, connectionID))

	reader, err := marshalBody(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, connectionPath(vpcID, connectionID), reader)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedUpdateVpnConnectionJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionUpdateVpnConnection, jobID); err != nil {
		return fmt.Errorf(ErrFmtUpdatePollingFailed, err)
	}
	return nil
}

// DeleteVpnConnection issues the removal and polls its job. A 409 that says the
// removal already runs goes on to the wait for the 404. Any other 409 reaches
// the caller as the API error, and a 404 as an error that client.IsNotFound
// reads.
func DeleteVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string) error {
	timeout := client.DefaultPollingConfig().Timeout
	if config := gpcnClient.Config(); config != nil && config.PollingTimeout > 0 {
		timeout = config.PollingTimeout
	}
	return deleteVpnConnection(gpcnClient, ctx, vpcID, connectionID, RemovalPollInterval, timeout)
}

func deleteVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string, interval, timeout time.Duration) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpnConnection, connectionID))
	startTime := time.Now()

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, connectionPath(vpcID, connectionID), nil)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if removalAlreadyRunning(err) {
		tflog.Info(ctx, LogRemovalAlreadyRunning)
		return waitForVpnConnectionGone(gpcnClient, ctx, vpcID, connectionID, interval, max(timeout-time.Since(startTime), 0))
	}
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteVpnConnectionJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteVpnConnection, jobID); err != nil {
		return fmt.Errorf(ErrFmtDeletePollingFailed, err)
	}
	return nil
}

// removalAlreadyRunning reports a 409 for a connection that is being removed
// and has no failure to show. A removal that failed is not under way.
func removalAlreadyRunning(err error) bool {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict || httpErr.Code != CodeStateConflict {
		return false
	}
	return httpErr.Details["state"] == StateRemoving && httpErr.Details["failureReason"] == nil
}

// waitForVpnConnectionGone reads the connection every interval until the API
// answers 404, or until the timeout.
func waitForVpnConnectionGone(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string, interval, timeout time.Duration) error {
	tflog.Info(ctx, fmt.Sprintf(LogWaitingForVpnConnectionGone, connectionID))

	startTime := time.Now()
	for {
		detail, err := GetVpnConnection(gpcnClient, ctx, vpcID, connectionID)
		if errors.Is(err, ErrVpnConnectionNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf(ErrFmtWaitForRemovalFailed, err)
		}
		tflog.Info(ctx, fmt.Sprintf(LogVpnConnectionStillRemoving, connectionID, detail.State))

		if time.Since(startTime) >= timeout {
			return fmt.Errorf("%w: "+ErrFmtRemovalTimeout, ErrRemovalTimeout, connectionID, detail.State, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// sendAccepted sends a request and returns the body of the 2xx answer.
func sendAccepted(send func(*http.Request) (*http.Response, error), request *http.Request) ([]byte, error) {
	response, err := send(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

// issueJob sends a request whose 202 carries a job ID in the singular job
// envelope.
func issueJob(gpcnClient *client.GpcnClient, request *http.Request) (string, error) {
	raw, err := sendAccepted(gpcnClient.DoWithRetry, request)
	if err != nil {
		return "", err
	}
	var accepted client.JobStatusSingularResponse
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return "", err
	}
	if accepted.Data.JobID == "" {
		return "", errors.New(ErrDetailNoJobIDInResponse)
	}
	return accepted.Data.JobID, nil
}
