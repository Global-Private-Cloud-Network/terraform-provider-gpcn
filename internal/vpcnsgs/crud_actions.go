package vpcnsgs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// ApiNsg is the group projection. It carries no VPC ID and no datacenter ID.
type ApiNsg struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	IsDefault     bool    `json:"isDefault"`
	State         string  `json:"state"`
	FailureReason *string `json:"failureReason"`
	RuleCount     int64   `json:"ruleCount"`
	SubnetCount   int64   `json:"subnetCount"`
	ActiveJobID   *string `json:"activeJobId"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

// ApiRule is one rule of a group. The API accepts an id on write and ignores
// it, because rule identity is the content tuple.
type ApiRule struct {
	ID           string  `json:"id"`
	Direction    string  `json:"direction"`
	Protocol     string  `json:"protocol"`
	PortRangeMin *int64  `json:"portRangeMin"`
	PortRangeMax *int64  `json:"portRangeMax"`
	RemoteCidr   string  `json:"remoteCidr"`
	Description  *string `json:"description"`
}

// NsgDetail is the shape the single read answers with. The listing puts the
// rules inside each row instead, so the two reads are not interchangeable.
type NsgDetail struct {
	Nsg   ApiNsg    `json:"nsg"`
	Rules []ApiRule `json:"rules"`
}

type createNsgResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		NsgID string `json:"nsgId"`
		JobID string `json:"jobId"`
	} `json:"data"`
}

type readNsgResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Data    NsgDetail `json:"data"`
}

func nsgCollectionPath(vpcID string) string {
	return BaseURLV1 + vpcID + nsgsPathSegment
}

func nsgPath(vpcID, nsgID string) string {
	return nsgCollectionPath(vpcID) + nsgID
}

// IssuedNsg is what the 202 answers with. GPCN inserts the group row before it
// dispatches the build job. The ID is therefore real whatever the job does
// next.
type IssuedNsg struct {
	JobID string
	NsgID string
}

// IssueCreateNsg posts the group with its rules inline and returns the 202
// without waiting. One call creates both, because the rules route is a replace
// rather than an append. The caller writes the ID to state before PollCreateNsg
// waits for the build.
func IssueCreateNsg(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, name, description string, rules []RuleModel) (*IssuedNsg, error) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, LogStartingCreateNsg)

	createNsgRequestBody := map[string]any{
		"name":        name,
		"description": description,
		"rules":       RuleRequestBodies(rules),
	}

	jsonCreateNsgRequestBody, err := json.Marshal(createNsgRequestBody)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "POST", nsgCollectionPath(vpcID), bytes.NewBuffer(jsonCreateNsgRequestBody))
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

	var created createNsgResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, err
	}
	tflog.Info(ctx, LogIssuedCreateNsgJob)

	return &IssuedNsg{JobID: created.Data.JobID, NsgID: created.Data.NsgID}, nil
}

// PollCreateNsg waits for the build job. It stands apart from the issue so the
// caller can put the group ID in state before the wait.
func PollCreateNsg(gpcnClient *client.GpcnClient, ctx context.Context, jobID string) error {
	ctx = client.WithCorrelationID(ctx)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionCreateNsg, jobID); err != nil {
		return fmt.Errorf("create security group polling failed: %w", err)
	}

	tflog.Info(ctx, LogLongPollingCompletedCreateNsg)
	return nil
}

// GetNsg reads the group and its complete rule set.
func GetNsg(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, nsgID string) (*NsgDetail, error) {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingGetNsgWithID, nsgID))

	request, err := http.NewRequestWithContext(ctx, "GET", nsgPath(vpcID, nsgID), nil)
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

	var read readNsgResponse
	if err := json.Unmarshal(body, &read); err != nil {
		return nil, err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRetrievedNsgWithID, nsgID))
	return &read.Data, nil
}

// RenameNsg relabels the group. The route is synchronous, because a name in
// this family never reaches the provider: objects are named from row IDs.
func RenameNsg(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, nsgID, name, description string) error {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingRenameNsgWithID, nsgID))

	renameRequestBody, err := json.Marshal(map[string]any{
		"name":        name,
		"description": description,
	})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", nsgPath(vpcID, nsgID), bytes.NewBuffer(renameRequestBody))
	if err != nil {
		return err
	}

	response, err := gpcnClient.DoWithRetry(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return err
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyRenamedNsg, nsgID))
	return nil
}

// ReplaceNsgRules sends the complete desired set. The API diffs it by content
// key, so a rule left out of the payload is removed from the group.
func ReplaceNsgRules(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, nsgID string, rules []RuleModel) error {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingReplaceNsgRules, nsgID))

	replaceRequestBody, err := json.Marshal(map[string]any{"rules": RuleRequestBodies(rules)})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, "PUT", nsgPath(vpcID, nsgID)+rulesPathSegment, bytes.NewBuffer(replaceRequestBody))
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedReplaceNsgRulesJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionReplaceNsgRules, jobID); err != nil {
		return fmt.Errorf("replace security group rules polling failed: %w", err)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyReplacedRules, nsgID))
	return nil
}

// DeleteNsg tears the group down. The VPC's own default group refuses, and so
// does a group any subnet still names.
func DeleteNsg(gpcnClient *client.GpcnClient, ctx context.Context, vpcID, nsgID string) error {
	ctx = client.WithCorrelationID(ctx)
	tflog.Info(ctx, fmt.Sprintf(LogStartingDeleteNsgWithID, nsgID))

	request, err := http.NewRequestWithContext(ctx, "DELETE", nsgPath(vpcID, nsgID), nil)
	if err != nil {
		return err
	}

	jobID, err := issueJob(gpcnClient, request)
	if err != nil {
		return err
	}
	tflog.Info(ctx, LogIssuedDeleteNsgJob)

	if _, err := client.PerformLongPolling(gpcnClient, ctx, ActionDeleteNsg, jobID); err != nil {
		return fmt.Errorf("delete security group polling failed: %w", err)
	}

	tflog.Info(ctx, fmt.Sprintf(LogSuccessfullyDeletedNsg, nsgID))
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
