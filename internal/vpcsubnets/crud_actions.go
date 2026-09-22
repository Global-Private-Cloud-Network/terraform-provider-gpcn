package vpcsubnets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ApiSubnet is the one projection the API uses for the listing, the create 202
// and the update 200. It carries no VPC ID and no datacenter ID.
type ApiSubnet struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Description      *string `json:"description"`
	CIDR             string  `json:"cidr"`
	State            string  `json:"state"`
	NsgID            string  `json:"nsgId"`
	NsgName          *string `json:"nsgName"`
	AttachedNicCount int64   `json:"attachedNicCount"`
	FailureReason    *string `json:"failureReason"`
	ActiveJobID      *string `json:"activeJobId"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type createSubnetResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		JobID  string    `json:"jobId"`
		Subnet ApiSubnet `json:"subnet"`
	} `json:"data"`
}

type listSubnetsResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    []ApiSubnet `json:"data"`
	Meta    struct {
		Total      int64 `json:"total"`
		Page       int64 `json:"page"`
		PageSize   int64 `json:"pageSize"`
		TotalPages int64 `json:"totalPages"`
	} `json:"meta"`
}

type updateSubnetResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Data    ApiSubnet `json:"data"`
}

func subnetCollectionPath(vpcID string) string {
	return BaseURLV1 + vpcID + subnetsPathSegment
}

func subnetPath(vpcID, subnetID string) string {
	return subnetCollectionPath(vpcID) + subnetID
}

// IssuedSubnet is what the 202 answers with. The platform inserts the row
// before it dispatches the carve job, so the ID, the reserved CIDR and the
// group binding are real whatever the job does next.
type IssuedSubnet struct {
	JobID  string
	Subnet ApiSubnet
}

// IssueCreateSubnet posts the subnet and returns the 202 without waiting. The
// caller writes the ID to state before PollCreateSubnet waits for the carve.
func IssueCreateSubnet(gpcnClient *client.GpcnClient, ctx context.Context, model ResourceModel) (*IssuedSubnet, error) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, LogStartingCreateSubnet)

	vpcID := model.VpcID.ValueString()
	createSubnetRequestBody := map[string]any{
		"name":        model.Name.ValueString(),
		"description": model.Description.ValueString(),
	}
	if !model.CIDR.IsNull() && !model.CIDR.IsUnknown() {
		createSubnetRequestBody["cidr"] = model.CIDR.ValueString()
	}
	if !model.Prefix.IsNull() && !model.Prefix.IsUnknown() {
		createSubnetRequestBody["prefix"] = model.Prefix.ValueInt64()
	}
	if !model.NsgID.IsNull() && !model.NsgID.IsUnknown() {
		createSubnetRequestBody["nsgId"] = model.NsgID.ValueString()
	}

	jsonCreateSubnetRequestBody, err := json.Marshal(createSubnetRequestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "POST", subnetCollectionPath(vpcID), bytes.NewBuffer(jsonCreateSubnetRequestBody))
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

	var created createSubnetResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateSubnetJob)

	return &IssuedSubnet{JobID: created.Data.JobID, Subnet: created.Data.Subnet}, nil
}

// PollCreateSubnet waits for the carve job. It stands apart from the issue so
// the caller can put the subnet ID in state before the wait.
func PollCreateSubnet(gpcnClient *client.GpcnClient, ctx context.Context, jobID string) error {
	ctx = client.WithCorrelationID(ctx)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateSubnet, jobID); err != nil {
		return fmt.Errorf("create subnet polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateSubnet)
	return nil
}

// GetSubnet pages the VPC's subnet listing and matches the ID. The API wires no
// single-read route for a subnet, so the listing is the only read there is.
func GetSubnet(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, subnetID string) (*ApiSubnet, error) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetSubnetWithID, subnetID))

	for page := int64(1); ; page++ {
		listing, err := listSubnetsPage(gpcnClient, ctx, vpcID, page)
		if err != nil {
			return nil, err
		}

		for index := range listing.Data {
			if listing.Data[index].ID == subnetID {
				tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedSubnetWithID, subnetID))
				return &listing.Data[index], nil
			}
		}

		// An empty page also stops the walk. A listing that reports fewer pages
		// than it serves would otherwise loop until the context expires.
		if page >= listing.Meta.TotalPages || len(listing.Data) == 0 {
			tflog.Info(ctx, fmt.Sprintf(LogSubnetAbsentFromListing, subnetID))
			return nil, ErrSubnetAbsent
		}
	}
}

func listSubnetsPage(gpcnClient *client.GpcnClient, ctx context.Context, vpcID string, page int64) (*listSubnetsResponse, error) {
	url := subnetCollectionPath(vpcID) + "?limit=" + strconv.Itoa(ListPageLimit) + "&page=" + strconv.FormatInt(page, 10)

	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	var listing listSubnetsResponse
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}
	return &listing, nil
}

// UpdateSubnet renames and relabels the subnet. The route is synchronous and
// answers with the whole row, because a rename here never reaches the provider.
func UpdateSubnet(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, subnetID, name, description string) (*ApiSubnet, error) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingUpdateSubnetWithID, subnetID))

	updateSubnetRequestBody := map[string]any{
		"name":        name,
		"description": description,
	}

	jsonUpdateSubnetRequestBody, err := json.Marshal(updateSubnetRequestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", subnetPath(vpcID, subnetID), bytes.NewBuffer(jsonUpdateSubnetRequestBody))
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

	var updated updateSubnetResponse
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyUpdatedSubnet, subnetID))
	return &updated.Data, nil
}

// RebindSubnetNsg moves the subnet to another security group. The row's group
// moves last, inside the workflow, so a failed job leaves the old binding.
func RebindSubnetNsg(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, subnetID, nsgID string) error {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingRebindSubnetNsg, subnetID, nsgID))

	rebindRequestBody, err := json.Marshal(map[string]any{"nsgId": nsgID})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", subnetPath(vpcID, subnetID)+nsgPathSegment, bytes.NewBuffer(rebindRequestBody))
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedRebindSubnetNsgJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionRebindNsg, jobID); err != nil {
		return fmt.Errorf("rebind subnet security group polling failed: %w", err)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRebondSubnetNsg, subnetID))
	return nil
}

// DeleteSubnet tears the subnet down. The API refuses a subnet that still holds
// live interfaces, and that refusal reaches the operator unchanged.
func DeleteSubnet(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, subnetID string) error {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteSubnetWithID, subnetID))

	request, err := http.NewRequestWithContext(ctx, "DELETE", subnetPath(vpcID, subnetID), nil)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteSubnetJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteSubnet, jobID); err != nil {
		return fmt.Errorf("delete subnet polling failed: %w", err)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyDeletedSubnet, subnetID))
	return nil
}

// issueJob sends a request whose 202 carries a job ID and nothing else. The
// request already carries the context, so this helper takes none.
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
	return accepted.Data.JobID, nil
}
