package vpcs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// vpcClientWithPollingTimeout builds a client with a short polling timeout.
// The shared test helper fixes the polling timeout at ten minutes.
func vpcClientWithPollingTimeout(t *testing.T, handler http.HandlerFunc, pollingTimeout time.Duration) *client.GpcnClient {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	config := client.DefaultConfig(server.URL, "test-key")
	config.MaxRetries = 0
	config.InitialRetryDelay = 0
	config.PollingTimeout = pollingTimeout

	gpcnClient, err := client.NewGpcnClient(config)
	if err != nil {
		t.Fatalf("failed to create GPCN client: %v", err)
	}
	return gpcnClient
}

// vpcBodyMock records the last request body of each write and answers every
// call with a VPC row, so Create and Update run to the end.
type vpcBodyMock struct {
	mutex sync.Mutex
	posts []map[string]any
	puts  []map[string]any
}

func (m *vpcBodyMock) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	row := map[string]any{
		"id": vpcUnitTestID, "name": "vpc-a", "cidr": "10.50.0.0/16", "status": VPC_STATUS_ACTIVE,
		"resourceGroupId": "rg-api", "createdAt": vpcUnitTestCreated, "updatedAt": vpcUnitTestUpdated,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			m.mutex.Lock()
			m.posts = append(m.posts, testutil.ReadRequestBody(r))
			m.mutex.Unlock()
			testutil.HandleCreateJobResponseWithResource(w, "job-create", row)
		case http.MethodPut:
			m.mutex.Lock()
			m.puts = append(m.puts, testutil.ReadRequestBody(r))
			m.mutex.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": row})
		case http.MethodGet:
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": row})
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}
}

func vpcBodyKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestCreateVpcSendsTheResourceGroupOnlyWhenKnownAndSet(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		group types.String
		want  []string
	}{
		"set":     {types.StringValue("rg-1"), []string{"cidr", "datacenterId", "name", "resourceGroupId"}},
		"null":    {types.StringNull(), []string{"cidr", "datacenterId", "name"}},
		"unknown": {types.StringUnknown(), []string{"cidr", "datacenterId", "name"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mock := &vpcBodyMock{}
			_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

			_, err := CreateVpc(gpcnClient, context.Background(), ResourceModel{
				Name:            types.StringValue("vpc-a"),
				CIDR:            types.StringValue("10.50.0.0/16"),
				DatacenterId:    types.StringValue("dc-1"),
				Description:     types.StringNull(),
				DNSNameservers:  types.ListNull(types.StringType),
				ResourceGroupId: tc.group,
			})
			if err != nil {
				t.Fatalf("CreateVpc: %v", err)
			}
			if len(mock.posts) != 1 {
				t.Fatalf("POST count = %d, want 1", len(mock.posts))
			}
			if got := vpcBodyKeys(mock.posts[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("create body keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpdateVpcSendsTheGroupAloneWhenOnlyTheGroupChanged(t *testing.T) {
	t.Parallel()

	state := ResourceModel{
		Name:            types.StringValue("vpc-a"),
		Description:     types.StringValue(""),
		ResourceGroupId: types.StringValue("rg-1"),
	}
	cases := map[string]struct {
		group types.String
		want  []string
	}{
		"moved":     {types.StringValue("rg-2"), []string{"resourceGroupId"}},
		"unchanged": {types.StringValue("rg-1"), nil},
		"unknown":   {types.StringUnknown(), nil},
		"null":      {types.StringNull(), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mock := &vpcBodyMock{}
			_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

			plan := state
			plan.ResourceGroupId = tc.group
			if _, err := UpdateVpc(gpcnClient, context.Background(), vpcUnitTestID, plan, state); err != nil {
				t.Fatalf("UpdateVpc: %v", err)
			}
			if tc.want == nil {
				if len(mock.puts) != 0 {
					t.Errorf("PUT bodies = %v, want none", mock.puts)
				}
				return
			}
			if len(mock.puts) != 1 {
				t.Fatalf("PUT count = %d, want 1", len(mock.puts))
			}
			if got := vpcBodyKeys(mock.puts[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("update body keys = %v, want %v", got, tc.want)
			}
		})
	}
}

// vpcAcceptedServer answers the VPC create and delete with the given bytes.
func vpcAcceptedServer(t *testing.T, method, body string) *client.GpcnClient {
	t.Helper()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(body))
		},
	})
	return gpcnClient
}

func vpcAcceptedTestModel() ResourceModel {
	return ResourceModel{
		Name:            types.StringValue("vpc-a"),
		CIDR:            types.StringValue("10.50.0.0/16"),
		DatacenterId:    types.StringValue("dc-1"),
		Description:     types.StringNull(),
		DNSNameservers:  types.ListNull(types.StringType),
		ResourceGroupId: types.StringNull(),
	}
}

func TestCreateVpcReadsAcceptedResource(t *testing.T) {
	t.Parallel()

	gpcnClient := vpcAcceptedServer(t, http.MethodPost, `{"success":true,"data":{`+
		`"job":{"id":"j1","operation":"create","resourceId":"v1","resourceName":"vpc-a","stage":"queued",`+
		`"outcome":null,"progress":0,"message":null,"failureReason":null},`+
		`"resource":{"id":"v1","name":"vpc-a","description":null,"cidr":"10.50.0.0/16",`+
		`"datacenter":{"id":"dc-1","code":null,"name":"Kansas"},"status":"creating","failureReason":null,`+
		`"egressIp":null,"createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z",`+
		`"dnsNameservers":[],"resourceGroupId":"rg-api"}}}`)

	created, err := CreateVpc(gpcnClient, context.Background(), vpcAcceptedTestModel())
	if err != nil {
		t.Fatalf("CreateVpc: %v", err)
	}
	if created.Data.Vpc.ID != "v1" {
		t.Errorf("row ID = %q, want %q", created.Data.Vpc.ID, "v1")
	}
	if created.Data.JobID != "j1" {
		t.Errorf("job ID = %q, want %q", created.Data.JobID, "j1")
	}
	if created.Data.Vpc.ResourceGroupID != "rg-api" {
		t.Errorf("row resource group = %q, want %q", created.Data.Vpc.ResourceGroupID, "rg-api")
	}
}

// A row the answer does not name cannot reach state, so the create stops.
func TestCreateVpcRefusesEmptyRowID(t *testing.T) {
	t.Parallel()

	gpcnClient := vpcAcceptedServer(t, http.MethodPost, `{"success":true,"data":{`+
		`"job":{"id":"j1","operation":"create","resourceId":null,"stage":"queued",`+
		`"outcome":null,"progress":0,"message":null,"failureReason":null},`+
		`"resource":{"name":"vpc-a","cidr":"10.50.0.0/16","status":"creating"}}}`)

	_, err := CreateVpc(gpcnClient, context.Background(), vpcAcceptedTestModel())
	if err == nil {
		t.Fatal("CreateVpc returned no error for a row without an ID")
	}
	if err.Error() != ErrDetailNoVpcIDInCreate {
		t.Errorf("error = %q, want %q", err.Error(), ErrDetailNoVpcIDInCreate)
	}
}

func TestIssueVpcDeleteReadsAcceptedJobID(t *testing.T) {
	t.Parallel()

	gpcnClient := vpcAcceptedServer(t, http.MethodDelete, `{"success":true,"data":{`+
		`"job":{"id":"j-delete","operation":"delete","resourceId":"v1","stage":"queued",`+
		`"outcome":null,"progress":0,"message":null,"failureReason":null},`+
		`"resource":{"id":"v1","name":"vpc-a","status":"deleting"}}}`)

	jobID, err := issueVpcDelete(gpcnClient, context.Background(), "v1")
	if err != nil {
		t.Fatalf("issueVpcDelete: %v", err)
	}
	if jobID != "j-delete" {
		t.Errorf("job ID = %q, want %q", jobID, "j-delete")
	}
}

// A job that holds the VPC refuses the delete with RESOURCE_BUSY. The delete
// goes out again once that job ends. The test changes a package variable, so
// it does not run in parallel.
func TestDeleteVpcWaitsOutBusyHolder(t *testing.T) {
	previous := BusyRetryInterval
	BusyRetryInterval = time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	const busyBody = `{"success":false,"message":"VPC is busy: create in progress.",` +
		`"error":{"code":"RESOURCE_BUSY","statusCode":409,"details":{"resourceType":"vpc",` +
		`"resourceId":"` + vpcUnitTestID + `","heldBy":{"jobId":"h1","operation":"create","verb":"create"}}}}`
	const acceptedBody = `{"success":true,"data":{` +
		`"job":{"id":"j-delete","operation":"delete","resourceId":"` + vpcUnitTestID + `","stage":"queued",` +
		`"outcome":null,"progress":0,"message":null,"failureReason":null},` +
		`"resource":{"id":"` + vpcUnitTestID + `","name":"vpc-a","status":"deleting"}}}`
	jobBody := func(jobID string) string {
		return `{"success":true,"data":{"jobs":[{"id":"` + jobID + `","operation":"delete","resourceId":"` + vpcUnitTestID + `",` +
			`"stage":"completed","outcome":"succeeded","progress":100,"message":null,"failureReason":null}],"notFound":[]}}`
	}

	var mutex sync.Mutex
	counts := map[string]int{}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case r.Method == http.MethodDelete && r.URL.Path == BASE_URL_V1+vpcUnitTestID:
				counts[http.MethodDelete]++
				if counts[http.MethodDelete] == 1 {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(busyBody))
					return
				}
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(acceptedBody))
			case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
				ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
				jobID, _ := ids[0].(string)
				counts["poll "+jobID]++
				_, _ = w.Write([]byte(jobBody(jobID)))
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID); err != nil {
		t.Fatalf("Expected the delete after the holder to succeed, got %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if counts[http.MethodDelete] != 2 {
		t.Errorf("DELETE count = %d, want 2", counts[http.MethodDelete])
	}
	if counts["poll h1"] != 1 || counts["poll j-delete"] != 1 {
		t.Errorf("polls = %v, want one of h1 and one of j-delete", counts)
	}
}

func TestMapVpcPayloadReadsHeldBy(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		keys string
		want types.String
	}{
		"held by a job":   {keys: `"heldBy":{"jobId":"j9","operation":"delete","verb":"delete"}`, want: types.StringValue("j9")},
		"held by nothing": {keys: `"heldBy":null`, want: types.StringNull()},
	} {
		var payload vpcPayload
		row := `{"id":"` + vpcUnitTestID + `","name":"vpc-a","status":"active",` + test.keys + `}`
		if err := json.Unmarshal([]byte(row), &payload); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		model := mapVpcPayloadToModel(context.Background(), payload, ResourceModel{Name: types.StringValue("vpc-a")})
		if !model.ActiveJobId.Equal(test.want) {
			t.Errorf("%s: active_job_id = %v, want %v", name, model.ActiveJobId, test.want)
		}
	}
}

// Each busy answer names a new holder that ends at once. The deadline is
// shared, so the chain still stops at it. The test changes a package
// variable, so it does not run in parallel.
func TestDeleteVpcBusyChainStopsAtDeadline(t *testing.T) {
	previous := BusyRetryInterval
	BusyRetryInterval = 10 * time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	const timeout = 300 * time.Millisecond
	var mutex sync.Mutex
	busy := 0
	gpcnClient := vpcClientWithPollingTimeout(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == BASE_URL_V1+vpcUnitTestID:
			mutex.Lock()
			busy++
			holder := "h" + strconv.Itoa(busy)
			mutex.Unlock()
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"success":false,"message":"VPC is busy: create in progress.",` +
				`"error":{"code":"RESOURCE_BUSY","statusCode":409,"details":{"resourceType":"vpc",` +
				`"resourceId":"` + vpcUnitTestID + `","heldBy":{"jobId":"` + holder + `","operation":"create","verb":"create"}}}}`))
		case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
			ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
			jobID, _ := ids[0].(string)
			_, _ = w.Write([]byte(`{"success":true,"data":{"jobs":[{"id":"` + jobID + `","operation":"create","resourceId":"` + vpcUnitTestID + `",` +
				`"stage":"completed","outcome":"succeeded","progress":100,"message":null,"failureReason":null}],"notFound":[]}}`))
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}, timeout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := DeleteVpc(gpcnClient, ctx, vpcUnitTestID)
	elapsed := time.Since(start)
	if !errors.Is(err, client.ErrBusyWaitExhausted) {
		t.Fatalf("Expected the busy wait error, got %v", err)
	}
	if elapsed >= 3*timeout {
		t.Errorf("Expected the chain to stop near the %s deadline, took %s", timeout, elapsed)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if busy < 2 {
		t.Errorf("Expected a chain of holders, got %d busy answers", busy)
	}
}

// vpcDeadlineServer answers the first DELETE with a 409 RESOURCE_BUSY whose
// holder ends after holderDelay. The next DELETE gets a 202 after
// acceptDelay, and its job never ends. The counter holds the DELETEs.
func vpcDeadlineServer(t *testing.T, holderDelay, acceptDelay, timeout time.Duration) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	previous := BusyRetryInterval
	BusyRetryInterval = 10 * time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	var deletes atomic.Int32
	gpcnClient := vpcClientWithPollingTimeout(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == BASE_URL_V1+vpcUnitTestID && deletes.Add(1) == 1:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"success":false,"message":"VPC is busy: create in progress.",` +
				`"error":{"code":"RESOURCE_BUSY","statusCode":409,"details":{"resourceType":"vpc",` +
				`"resourceId":"` + vpcUnitTestID + `","heldBy":{"jobId":"h1","operation":"create","verb":"create"}}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == BASE_URL_V1+vpcUnitTestID:
			time.Sleep(acceptDelay)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"success":true,"data":{` +
				`"job":{"id":"j-delete","operation":"delete","resourceId":"` + vpcUnitTestID + `","stage":"queued",` +
				`"outcome":null,"progress":0,"message":null,"failureReason":null},` +
				`"resource":{"id":"` + vpcUnitTestID + `","name":"vpc-a","status":"deleting"}}}`))
		case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
			ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
			jobID, _ := ids[0].(string)
			stage, outcome := `"running"`, "null"
			if jobID == "h1" {
				time.Sleep(holderDelay)
				stage, outcome = `"completed"`, `"succeeded"`
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"jobs":[{"id":"` + jobID + `","operation":"delete","resourceId":"` + vpcUnitTestID + `",` +
				`"stage":` + stage + `,"outcome":` + outcome + `,"progress":50,"message":null,"failureReason":null}],"notFound":[]}}`))
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}, timeout)
	return gpcnClient, &deletes
}

// The holder ends after the deadline, so the delete does not go out again. The
// test changes a package variable, so it does not run in parallel.
func TestDeleteVpcSendsNothingAfterTheDeadline(t *testing.T) {
	gpcnClient, deletes := vpcDeadlineServer(t, 450*time.Millisecond, 0, 400*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := DeleteVpc(gpcnClient, ctx, vpcUnitTestID)
	if !errors.Is(err, client.ErrBusyWaitExhausted) {
		t.Fatalf("Expected the busy wait error, got %v", err)
	}
	if errors.Is(err, client.ErrHolderStillRunning) {
		t.Errorf("Expected an error that does not say the holder still runs, got %v", err)
	}
	if want := "job h1 ended as the wait for it ran out after "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Expected the error to start with %q, got %q", want, err.Error())
	}
	if got := deletes.Load(); got != 1 {
		t.Errorf("DELETE count = %d, want 1", got)
	}
	if ctx.Err() != nil {
		t.Errorf("Expected the call to end before the context deadline, got %v", ctx.Err())
	}
}

// The holder ends before the deadline, and the deadline passes while the
// second DELETE waits for its 202. The job poll then gets what is left of the
// polling timeout. The test changes a package variable, so it does not run in
// parallel.
func TestDeleteVpcPollSharesTheDeadlineAfterABusyWait(t *testing.T) {
	const timeout = 400 * time.Millisecond
	gpcnClient, deletes := vpcDeadlineServer(t, 250*time.Millisecond, 200*time.Millisecond, timeout)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := DeleteVpc(gpcnClient, ctx, vpcUnitTestID)
	elapsed := time.Since(start)
	if !errors.Is(err, client.ErrLongPollingTimeout) {
		t.Fatalf("Expected the polling timeout, got %v", err)
	}
	if elapsed >= 3*timeout {
		t.Errorf("Expected the job poll to stop near the %s deadline, took %s", timeout, elapsed)
	}
	if got := deletes.Load(); got != 2 {
		t.Errorf("DELETE count = %d, want 2", got)
	}
}
