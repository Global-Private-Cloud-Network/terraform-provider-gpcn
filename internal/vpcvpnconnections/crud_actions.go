package vpcvpnconnections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"

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

// CreateVpnConnection posts the VPN connection and polls its job. It returns
// the connection ID as soon as the API accepts the POST, even when the job then
// fails, so the caller can read the row the job left. The ID is empty when the
// API refused the POST.
func CreateVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, body map[string]any) (string, error) {
	tflog.Info(ctx, fmt.Sprintf(LogStartingCreateVpnConnection, vpcID))

	reader, err := marshalBody(body)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, collectionPath(vpcID), reader)
	if err != nil {
		return "", err
	}

	raw, err := sendAccepted(gpcnClient, request)
	if err != nil {
		return "", err
	}
	var accepted createVpnConnectionResponse
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return "", err
	}
	if accepted.Data.JobID == "" {
		return "", errors.New(ErrDetailNoJobIDInResponse)
	}
	connectionID := accepted.Data.VpnConnection.ID
	if connectionID == "" {
		return "", errors.New(ErrDetailNoConnectionID)
	}
	tflog.Info(ctx, LogIssuedCreateVpnConnectionJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateVpnConnection, accepted.Data.JobID); err != nil {
		return connectionID, fmt.Errorf(ErrFmtCreatePollingFailed, err)
	}
	return connectionID, nil
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

// DeleteVpnConnection issues the removal and polls its job. A 409 reaches the
// caller as the API error, and a 404 as an error that client.IsNotFound reads.
func DeleteVpnConnection(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, connectionID string) error {
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteVpnConnection, connectionID))

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, connectionPath(vpcID, connectionID), nil)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteVpnConnectionJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteVpnConnection, jobID); err != nil {
		return fmt.Errorf(ErrFmtDeletePollingFailed, err)
	}
	return nil
}

// sendAccepted sends a request and returns the body of the 2xx answer.
func sendAccepted(gpcnClient *client.GpcnClient, request *http.Request) ([]byte, error) {
	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

// issueJob sends a request whose 202 carries a job ID in the singular job
// envelope.
func issueJob(gpcnClient *client.GpcnClient, request *http.Request) (string, error) {
	raw, err := sendAccepted(gpcnClient, request)
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
