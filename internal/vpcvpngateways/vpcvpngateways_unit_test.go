package vpcvpngateways

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testVpcID     = "11111111-1111-4111-8111-111111111111"
	testGatewayID = "22222222-2222-4222-8222-222222222222"
)

func ptr(value string) *string { return &value }

func TestVpcVpnGatewayUnitMapGatewayToModelRouterKindHasNullReservedBlock(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{
		ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateReady,
		Address: ptr("198.51.100.7"),
	}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if model.ID.ValueString() != testGatewayID {
		t.Errorf("Expected id %q, got %q", testGatewayID, model.ID.ValueString())
	}
	if model.Kind.ValueString() != KindRouter {
		t.Errorf("Expected kind %q, got %q", KindRouter, model.Kind.ValueString())
	}
	if model.Address.ValueString() != "198.51.100.7" {
		t.Errorf("Expected address 198.51.100.7, got %q", model.Address.ValueString())
	}
	if !model.ReservedBlock.IsNull() {
		t.Errorf("Expected a null reserved_block on the router kind, got %v", model.ReservedBlock)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("Expected a null failure_reason, got %v", model.FailureReason)
	}
	if model.State.ValueString() != StateReady {
		t.Errorf("Expected state %q, got %q", StateReady, model.State.ValueString())
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelApplianceKindCarriesReservedBlock(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{
		ID: testGatewayID, VpcID: testVpcID, Kind: KindAppliance, State: StateReady,
		Address: ptr("203.0.113.9"), ReservedBlock: ptr("10.112.255.240/28"),
	}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if model.ReservedBlock.ValueString() != "10.112.255.240/28" {
		t.Errorf("Expected reserved_block 10.112.255.240/28, got %v", model.ReservedBlock)
	}
	if model.Kind.ValueString() != KindAppliance {
		t.Errorf("Expected kind %q, got %q", KindAppliance, model.Kind.ValueString())
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelKeepsNullAddressWhileCreating(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateCreating}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if !model.Address.IsNull() {
		t.Errorf("Expected a null address while the gateway is creating, got %v", model.Address)
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelFillsVpcIDOnImport(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateReady}

	model := MapGatewayToModel(response, ResourceModel{})

	if model.VpcID.ValueString() != testVpcID {
		t.Errorf("Expected vpc_id %q, got %q", testVpcID, model.VpcID.ValueString())
	}
}

// errorServer answers every request with one status and body. The real
// transport stays in the stack, so the error arrives as *client.HTTPError.
func errorServer(t *testing.T, status int, body string) *client.GpcnClient {
	t.Helper()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		},
	})
	return gpcnClient
}

func TestVpcVpnGatewayUnitParseAPIErrorReturnsMessageVerbatimAndCode(t *testing.T) {
	t.Parallel()
	const message = "Remove the VPC's VPN connections before removing its VPN gateway."
	gpcnClient := errorServer(t, http.StatusConflict,
		`{"success":false,"message":"`+message+`","error":{"code":"VPN_GATEWAY_IN_USE","statusCode":409,"details":null}}`)

	err := DeleteVpnGateway(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatal("Expected the 409 to fail the delete")
	}

	code, got := ParseAPIError(err)
	if code != "VPN_GATEWAY_IN_USE" {
		t.Errorf("Expected code VPN_GATEWAY_IN_USE, got %q", code)
	}
	if got != message {
		t.Errorf("Expected message %q, got %q", message, got)
	}
	if detail := ErrorDetail(err); detail != message {
		t.Errorf("Expected the detail to be the message alone, got %q", detail)
	}
}

func TestVpcVpnGatewayUnitParseAPIErrorOnNonAPIErrorReadsEmpty(t *testing.T) {
	t.Parallel()
	err := errors.New("connection refused")

	code, message := ParseAPIError(err)
	if code != "" || message != "" {
		t.Errorf("Expected an empty code and message, got %q and %q", code, message)
	}
	if detail := ErrorDetail(err); detail != "connection refused" {
		t.Errorf("Expected the error text for a non-API error, got %q", detail)
	}
}

func TestVpcVpnGatewayUnitParseAPIErrorKeepsDataCenterRefusalMessage(t *testing.T) {
	t.Parallel()
	const message = "This data center does not offer site-to-site VPN yet."
	gpcnClient := errorServer(t, http.StatusConflict,
		`{"success":false,"message":"`+message+`","error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`)

	rowMayExist, _, err := CreateVpnGateway(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatal("Expected the 409 to fail the create")
	}
	if rowMayExist {
		t.Error("Expected a refused POST to report that no row exists")
	}
	if detail := ErrorDetail(err); detail != message {
		t.Errorf("Expected %q, got %q", message, detail)
	}
}

func TestVpcVpnGatewayUnitGetVpnGatewayMapsNotFoundToSentinel(t *testing.T) {
	t.Parallel()
	gpcnClient := errorServer(t, http.StatusNotFound,
		`{"success":false,"message":"This VPC has no VPN gateway.","error":{"code":"VPN_GATEWAY_NOT_FOUND","statusCode":404,"details":null}}`)

	_, err := GetVpnGateway(gpcnClient, context.Background(), testVpcID)
	if !errors.Is(err, ErrVpnGatewayNotFound) {
		t.Fatalf("Expected ErrVpnGatewayNotFound, got %v", err)
	}
	if !client.IsNotFound(err) {
		t.Errorf("Expected the API error to stay in the chain, got %v", err)
	}
}

func TestVpcVpnGatewayUnitCreateVpnGatewaySendsEmptyObjectBody(t *testing.T) {
	t.Parallel()
	var createBody atomic.Value
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/vpn-gateway":
				createBody.Store(testutil.ReadRequestBody(r))
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Operation initiated successfully",
					"data": map[string]any{"jobId": "job-create", "vpnGateway": map[string]any{"id": testGatewayID, "state": "creating"}},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, "job-create", testGatewayID, true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if _, _, err := CreateVpnGateway(gpcnClient, context.Background(), testVpcID); err != nil {
		t.Fatalf("Expected the create to succeed, got %v", err)
	}
	body, _ := createBody.Load().(map[string]any)
	if body == nil || len(body) != 0 {
		t.Errorf("Expected an empty JSON object body, got %v", body)
	}
}

// removalServer serves the gateway the way a removal looks from outside. Each
// read takes the next scripted answer, and the last one repeats.
func removalServer(t *testing.T, answers []string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/resource/vpcs/"+testVpcID+"/vpn-gateway" {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			index := int(reads.Add(1)) - 1
			if index >= len(answers) {
				index = len(answers) - 1
			}
			answer := answers[index]
			w.Header().Set("Content-Type", "application/json")
			if answer == "gone" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"This VPC has no VPN gateway.","error":{"code":"VPN_GATEWAY_NOT_FOUND","statusCode":404,"details":null}}`)
				return
			}
			fmt.Fprint(w, answer)
		},
	})
	return gpcnClient, &reads
}

func removingRow(reason string) string {
	failure := "null"
	if reason != "" {
		failure = `"` + reason + `"`
	}
	return `{"success":true,"message":"","data":{"id":"` + testGatewayID + `","vpcId":"` + testVpcID +
		`","kind":"router","state":"removing","address":"198.51.100.7","reservedBlock":null,"failureReason":` + failure +
		`,"activeJobId":null,"createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z"}}`
}

func TestVpcVpnGatewayUnitWaitForVpnGatewayGoneTimeoutNamesLastStateAndFailureReason(t *testing.T) {
	t.Parallel()
	const reason = "The VPN gateway's removal failed; it is retried automatically."
	gpcnClient, _ := removalServer(t, []string{removingRow(""), removingRow(reason)})

	err := waitForVpnGatewayGone(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Now(), 20*time.Millisecond)
	if !errors.Is(err, ErrRemovalTimeout) {
		t.Fatalf("Expected ErrRemovalTimeout, got %v", err)
	}
	for _, want := range []string{"'removing'", "after 20ms", reason, testVpcID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the timeout error to contain %q, got %q", want, err.Error())
		}
	}
}

func TestVpcVpnGatewayUnitWaitForVpnGatewayGoneStopsOnReadError(t *testing.T) {
	t.Parallel()
	gpcnClient := errorServer(t, http.StatusForbidden,
		`{"success":false,"message":"You do not have permission to perform this action.","error":{"code":"Insufficient Permissions","statusCode":403,"details":null}}`)

	err := waitForVpnGatewayGone(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Now(), time.Minute)
	if err == nil {
		t.Fatal("Expected the 403 to stop the wait")
	}
	if got := ErrorDetail(err); got != "You do not have permission to perform this action." {
		t.Errorf("Expected the API message, got %q", got)
	}
}

// deleteServer serves a delete. The DELETE answers deleteStatus, the job poll
// reports a failed job, and the gateway reads follow the scripted answers.
func deleteServer(t *testing.T, deleteStatus int, answers []string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			path := "/v1/resource/vpcs/" + testVpcID + "/vpn-gateway"
			switch {
			case r.Method == http.MethodDelete && r.URL.Path == path && deleteStatus == http.StatusConflict:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"message":"The VPN gateway is already being removed.","error":{"code":"VPN_GATEWAY_REMOVING","statusCode":409,"details":null}}`)
			case r.Method == http.MethodDelete && r.URL.Path == path:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Operation initiated successfully",
					"data": map[string]any{"jobId": "job-delete"},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Data: client.JobStatusDataResponse{Jobs: []client.JobResponse{{
						JobID: "job-delete", HasFailed: true, ErrorMessage: "removal failed",
					}}},
				})
			case r.Method == http.MethodGet && r.URL.Path == path:
				index := min(int(reads.Add(1))-1, len(answers)-1)
				w.Header().Set("Content-Type", "application/json")
				if answers[index] == "gone" {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"success":false,"message":"This VPC has no VPN gateway.","error":{"code":"VPN_GATEWAY_NOT_FOUND","statusCode":404,"details":null}}`)
					return
				}
				fmt.Fprint(w, answers[index])
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	return gpcnClient, &reads
}

func TestVpcVpnGatewayUnitWaitAfterFailedDeleteJob(t *testing.T) {
	t.Parallel()
	gpcnClient, reads := deleteServer(t, http.StatusAccepted, []string{
		removingRow("The VPN gateway's removal failed; it is retried automatically."),
		removingRow("The VPN gateway's removal failed; it is retried automatically."),
		"gone",
	})

	if err := deleteVpnGateway(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Minute); err != nil {
		t.Fatalf("Expected the failed job to fall through to the wait and end at the 404, got %v", err)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("Expected 3 reads, got %d", got)
	}
}

func TestVpcVpnGatewayUnitDeleteRemoving409FallsThroughToWait(t *testing.T) {
	t.Parallel()
	gpcnClient, reads := deleteServer(t, http.StatusConflict, []string{removingRow(""), "gone"})

	if err := deleteVpnGateway(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Minute); err != nil {
		t.Fatalf("Expected the 409 to fall through to the wait, got %v", err)
	}
	if got := reads.Load(); got != 2 {
		t.Errorf("Expected 2 reads, got %d", got)
	}
}

// A canceled context fails the read of a row, so ReadAfterFailure reads on a
// fresh context.
func TestVpcVpnGatewayUnitReadAfterFailureSurvivesCanceledContext(t *testing.T) {
	t.Parallel()
	gpcnClient, _ := removalServer(t, []string{removingRow("")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gateway, err := ReadAfterFailure(gpcnClient, ctx, testVpcID)
	if err != nil || gateway == nil || gateway.ID != testGatewayID {
		t.Fatalf("Expected the row with a canceled context, got %+v, %v", gateway, err)
	}
	if _, err := GetVpnGateway(gpcnClient, ctx, testVpcID); err == nil {
		t.Error("Expected a plain read on the canceled context to fail")
	}
}

func TestVpcVpnGatewayUnitCreatedSince(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		createdAt string
		want      bool
	}{
		"after the request":         {"2026-10-16T12:00:03.250Z", true},
		"inside the window":         {"2026-10-16T11:58:30Z", true},
		"the edge of the window":    {"2026-10-16T11:58:00Z", true},
		"before the window":         {"2026-10-16T11:57:59Z", false},
		"a row from an earlier day": {"2026-01-02T15:04:05Z", false},
		"empty":                     {"", false},
		"not a time":                {"yesterday", false},
	} {
		if got := CreatedSince(test.createdAt, start); got != test.want {
			t.Errorf("%s: CreatedSince(%q) = %v, want %v", name, test.createdAt, got, test.want)
		}
	}
}

func TestVpcVpnGatewayUnitRowMayExist(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err  error
		want bool
	}{
		"a transport error":      {errors.New("connection reset"), true},
		"a wrapped plain error":  {fmt.Errorf("send: %w", errors.New("EOF")), true},
		"a 5xx answer":           {&client.HTTPError{StatusCode: http.StatusBadGateway}, true},
		"a 4xx refusal":          {&client.HTTPError{StatusCode: http.StatusConflict}, false},
		"a wrapped 4xx refusal":  {fmt.Errorf("send: %w", &client.HTTPError{StatusCode: http.StatusForbidden}), false},
		"a wrapped 5xx answer":   {fmt.Errorf("send: %w", &client.HTTPError{StatusCode: http.StatusServiceUnavailable}), true},
		"a 429 rate limit":       {&client.HTTPError{StatusCode: http.StatusTooManyRequests}, false},
		"the retry limit ending": {fmt.Errorf("%w: %w", client.ErrMaxRetriesExceeded, errors.New("EOF")), true},
	} {
		if got := RowMayExist(test.err); got != test.want {
			t.Errorf("%s: RowMayExist = %v, want %v", name, got, test.want)
		}
	}
}

// pollCountServer answers every job poll with a job that does not end, and
// counts the polls.
func pollCountServer(t *testing.T) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	var polls atomic.Int32
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/resource/jobs/" {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			polls.Add(1)
			testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
				Success: true,
				Data:    client.JobStatusDataResponse{Jobs: []client.JobResponse{{JobID: "job-live", Stage: "running"}}},
			})
		},
	})
	return gpcnClient, &polls
}

// The adopted job shares the deadline of the request. A request that started
// longer ago than the polling timeout leaves no time, so the job is read once
// and the poll then stops without waiting.
func TestVpcVpnGatewayUnitPollAdoptedJobSpentDeadlineReadsOnce(t *testing.T) {
	t.Parallel()
	gpcnClient, polls := pollCountServer(t)
	gpcnClient.Config().PollingTimeout = time.Minute

	started := time.Now()
	PollAdoptedJob(gpcnClient, context.Background(), ActionCreateVpnGateway, ptr("job-live"), time.Now().Add(-time.Hour))

	if got := polls.Load(); got != 1 {
		t.Errorf("Expected one poll with a spent deadline, got %d", got)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Errorf("Expected no backoff wait with a spent deadline, took %s", elapsed)
	}
}

func TestVpcVpnGatewayUnitPollAdoptedJobWithoutJobDoesNotPoll(t *testing.T) {
	t.Parallel()
	gpcnClient, polls := pollCountServer(t)

	PollAdoptedJob(gpcnClient, context.Background(), ActionCreateVpnGateway, nil, time.Now())
	PollAdoptedJob(gpcnClient, context.Background(), ActionCreateVpnGateway, ptr(""), time.Now())

	if got := polls.Load(); got != 0 {
		t.Errorf("Expected no poll without an active job, got %d", got)
	}
}

// creatingRow is a gateway that has not finished its create.
func creatingRow() string {
	return `{"success":true,"message":"","data":{"id":"` + testGatewayID + `","vpcId":"` + testVpcID +
		`","kind":"router","state":"creating","address":null,"reservedBlock":null,"failureReason":null,` +
		`"activeJobId":null,"createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z"}}`
}

// A delete that waits for a create shares the deadline of the removal. A
// create that never ends ends the delete with the removal timeout error.
func TestVpcVpnGatewayUnitDeleteWhileCreatingTimesOut(t *testing.T) {
	t.Parallel()
	var deletes atomic.Int32
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.Method {
			case http.MethodDelete:
				deletes.Add(1)
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"message":"The VPN gateway is still being created.","error":{"code":"VPN_GATEWAY_BUSY_CREATING","statusCode":409,"details":null}}`)
			case http.MethodGet:
				fmt.Fprint(w, creatingRow())
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	err := deleteVpnGateway(gpcnClient, context.Background(), testVpcID, time.Millisecond, 20*time.Millisecond)
	if want := fmt.Sprintf(ErrFmtStillCreating, testVpcID, 20*time.Millisecond); err == nil || err.Error() != want {
		t.Fatalf("Expected %q, got %v", want, err)
	}
	if got := deletes.Load(); got != 1 {
		t.Errorf("Expected one DELETE before the timeout, got %d", got)
	}
}

// A gateway that leaves the creating state only for the next DELETE to meet
// 409 again must not keep the loop going past the shared deadline.
func TestVpcVpnGatewayUnitDeleteRepeatedBusyStopsAtDeadline(t *testing.T) {
	t.Parallel()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.Method {
			case http.MethodDelete:
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"message":"The VPN gateway is still being created.","error":{"code":"VPN_GATEWAY_BUSY_CREATING","statusCode":409,"details":null}}`)
			case http.MethodGet:
				fmt.Fprint(w, removingRow(""))
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := deleteVpnGateway(gpcnClient, ctx, testVpcID, time.Millisecond, 20*time.Millisecond)
	if want := fmt.Sprintf(ErrFmtStillCreating, testVpcID, 20*time.Millisecond); err == nil || err.Error() != want {
		t.Fatalf("Expected %q, got %v", want, err)
	}
}

// A read that fails while the delete waits for a create reports the create
// wait, not the removal wait.
func TestVpcVpnGatewayUnitDeleteWhileCreatingReadFailureNamesTheCreateWait(t *testing.T) {
	t.Parallel()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.Method {
			case http.MethodDelete:
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"message":"The VPN gateway is still being created.","error":{"code":"VPN_GATEWAY_BUSY_CREATING","statusCode":409,"details":null}}`)
			case http.MethodGet:
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"success":false,"message":"forbidden","error":{"code":"FORBIDDEN","statusCode":403,"details":null}}`)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	err := deleteVpnGateway(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Minute)
	prefix := strings.TrimSuffix(ErrFmtWaitForCreateFailed, "%w")
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("Expected an error that starts with %q, got %v", prefix, err)
	}
}

// The removal timeout names the configured total, not what was left when the
// wait started.
func TestVpcVpnGatewayUnitDeleteTimeoutNamesTheConfiguredTotal(t *testing.T) {
	t.Parallel()
	const reason = "The VPN gateway's removal failed; it is retried automatically."
	gpcnClient, _ := deleteServer(t, http.StatusAccepted, []string{removingRow(reason)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := deleteVpnGateway(gpcnClient, ctx, testVpcID, time.Millisecond, 100*time.Millisecond)
	want := fmt.Sprintf("%v: "+ErrFmtRemovalTimeout, ErrRemovalTimeout, testVpcID, "removing", 100*time.Millisecond, reason)
	if err == nil || err.Error() != want {
		t.Fatalf("Expected %q, got %v", want, err)
	}
}
