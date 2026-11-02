package vpcs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const vpcNotActiveBody = `{"success":false,` +
	`"message":"The VPC is not active (status: creating); this operation needs an active VPC.",` +
	`"error":{"code":"VPC_NOT_ACTIVE","statusCode":409,"details":null}}`

// A row that no longer exists answers with these bytes.
const vpcGoneBody = `{"success":false,"message":"VPC not found",` +
	`"error":{"code":"Resource Not Found","statusCode":404,"details":null}}`

// A gateway between the provider and the API answers for itself, so the body
// carries no GPCN envelope.
const vpcGatewayFailureBody = "Bad Gateway"

// vpcDeleteMock answers the first delete with the refusal a creating VPC
// raises, and the second with the teardown job.
type vpcDeleteMock struct {
	mutex sync.Mutex
	// refusals is how many DELETEs answer VPC_NOT_ACTIVE before one is taken.
	refusals    int
	status      string
	activeJobID any
	deletes     int
	// gets counts the reads of the row.
	gets int
	// readGone is true when the row has already gone.
	readGone bool
	// failReadsFrom is the read from which the gateway answers instead of the
	// row. Zero means every read reaches the API.
	failReadsFrom int
}

// deleteCount reads the counter the handler goroutine writes. The mutex orders
// the two, because the race detector fails a build on an unguarded read.
func (m *vpcDeleteMock) deleteCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.deletes
}

func (m *vpcDeleteMock) handler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			m.mutex.Lock()
			m.deletes++
			refuse := m.deletes <= m.refusals
			m.mutex.Unlock()

			if refuse {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(vpcNotActiveBody))
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"jobId": "job-delete"},
			})
		case r.Method == http.MethodGet:
			m.mutex.Lock()
			m.gets++
			status := m.status
			activeJobID := m.activeJobID
			gone := m.readGone
			failed := m.failReadsFrom > 0 && m.gets >= m.failReadsFrom
			m.mutex.Unlock()

			if failed {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(vpcGatewayFailureBody))
				return
			}
			if gone {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(vpcGoneBody))
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id":              vpcUnitTestID,
					"resourceGroupId": "rg-api",
					"name":            "vpc-creating",
					"cidr":            "10.50.0.0/16",
					"status":          status,
					"activeJobId":     activeJobID,
					"createdAt":       vpcUnitTestCreated,
					"updatedAt":       vpcUnitTestUpdated,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
			testutil.HandleJobResponse(w, "job-create", vpcUnitTestID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}
}

// The refusal carries no status of its own, so the row has to answer for
// itself. A VPC that is still being created clears the refusal as soon as its
// create job ends.
func TestVpcDeleteWaitsForTheCreateJobAndRetriesOnce(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{refusals: 1, status: VPC_STATUS_CREATING, activeJobID: "job-create"}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	if err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID); err != nil {
		t.Fatalf("Expected the retried delete to succeed, got %v", err)
	}
	if count := mock.deleteCount(); count != 2 {
		t.Errorf("DELETE count = %d, want 2", count)
	}
}

// Nothing the provider can wait for clears a refusal on an active VPC, so the
// refusal reaches the reader.
func TestVpcDeleteDoesNotRetryWhenNoJobOwnsTheVpc(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{refusals: 1, status: VPC_STATUS_ACTIVE, activeJobID: nil}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	if err == nil {
		t.Fatalf("Expected the refusal to reach the caller")
	}
	if !client.HasErrorCode(err, ERROR_CODE_VPC_NOT_ACTIVE) {
		t.Errorf("Expected a VPC_NOT_ACTIVE error, got %v", err)
	}
	if count := mock.deleteCount(); count != 1 {
		t.Errorf("DELETE count = %d, want 1", count)
	}
}

// The retry exists for one race: the create job ends between the refusal and
// the second DELETE. A refusal that survives it is the platform's answer, and
// a loop around it hides a VPC nothing will free.
func TestVpcDeleteSurfacesASecondRefusal(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{refusals: 2, status: VPC_STATUS_CREATING, activeJobID: "job-create"}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	if err == nil {
		t.Fatalf("Expected the second refusal to reach the caller")
	}
	if !client.HasErrorCode(err, ERROR_CODE_VPC_NOT_ACTIVE) {
		t.Errorf("Expected a VPC_NOT_ACTIVE error, got %v", err)
	}
	if count := mock.deleteCount(); count != 2 {
		t.Errorf("DELETE count = %d, want 2", count)
	}
}

// The ruled bytes for a wait that runs out, with the measured duration left
// open. A test that reads the constant cannot refute a rewrite of it.
var vpcTeardownTimeoutPattern = regexp.MustCompile(
	`a teardown was already running, and the VPC was still present after [0-9]\S*`)

// vpcTeardownTestCap bounds the wait this test itself makes. A provider that
// never ends the wait fails here instead of hanging the package.
const vpcTeardownTestCap = 10 * time.Second

// vpcClientWithPollingTimeout builds a client the timeout arm can reach. The
// shared test helper fixes the polling timeout at ten minutes.
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

// A teardown another caller started can outlive the polling timeout. The
// provider then reports the wait instead of a destroy it never achieves.
func TestDeleteVpcReportsATeardownThatOutlivesThePollingTimeout(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{refusals: 1, status: VPC_STATUS_DELETING, activeJobID: nil}
	gpcnClient := vpcClientWithPollingTimeout(t, mock.handler(t), 5*time.Millisecond)

	deleteResult := make(chan error, 1)
	go func() {
		deleteResult <- DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	}()

	select {
	case err := <-deleteResult:
		if err == nil {
			t.Fatalf("Expected the wait to run out and report itself")
		}
		if !vpcTeardownTimeoutPattern.MatchString(err.Error()) {
			t.Errorf("error = %q, want a match for %s", err.Error(), vpcTeardownTimeoutPattern)
		}
		if count := mock.deleteCount(); count != 1 {
			t.Errorf("DELETE count = %d, want 1", count)
		}
	case <-time.After(vpcTeardownTestCap):
		t.Fatalf("DeleteVpc did not return within %s", vpcTeardownTestCap)
	}
}

// The teardown another caller started can end between the refusal and the
// status read. The row is gone, which is the state the destroy asked for.
func TestVpcDeleteTreatsARowThatVanishedAsDeleted(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{refusals: 1, status: VPC_STATUS_DELETING, readGone: true}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	if err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID); err != nil {
		t.Fatalf("Expected a row that vanished to count as deleted, got %v", err)
	}
	if count := mock.deleteCount(); count != 1 {
		t.Errorf("DELETE count = %d, want 1", count)
	}
}

// The status read decides what the refusal means. A read that fails decides
// nothing, and the refusal names a state the provider never confirmed.
func TestVpcDeleteSurfacesAFailedStatusReadAfterTheRefusal(t *testing.T) {
	t.Parallel()

	// The status read is the first read of the row.
	mock := &vpcDeleteMock{refusals: 1, failReadsFrom: 1}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	if err == nil {
		t.Fatalf("Expected the failed status read to reach the caller")
	}
	if client.IsNotFound(err) {
		t.Errorf("Expected a failure that differs from a row that is gone, got %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q, want the gateway status in it", err.Error())
	}
	if count := mock.deleteCount(); count != 1 {
		t.Errorf("DELETE count = %d, want 1", count)
	}
}

// A read that fails mid-wait says nothing about the teardown. A provider that
// took the failure for the 404 would drop a live VPC from state.
func TestVpcDeleteSurfacesAFailedReadDuringTheTeardownWait(t *testing.T) {
	t.Parallel()

	// The status read is the first, so the wait meets the gateway on its own
	// first read.
	mock := &vpcDeleteMock{refusals: 1, status: VPC_STATUS_DELETING, failReadsFrom: 2}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	if err == nil {
		t.Fatalf("Expected the failed read to reach the caller")
	}
	if client.IsNotFound(err) {
		t.Errorf("Expected a failure that differs from a row that is gone, got %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q, want the gateway status in it", err.Error())
	}
	if count := mock.deleteCount(); count != 1 {
		t.Errorf("DELETE count = %d, want 1", count)
	}
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
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"jobId": "job-create", "vpc": row}})
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

// The runtime 202 carries the job as data.job and the row as data.resource.
func TestCreateVpcReadsAcceptedResource(t *testing.T) {
	t.Parallel()

	gpcnClient := vpcAcceptedServer(t, http.MethodPost, `{"success":true,"data":{`+
		`"job":{"id":"j1","operation":"create","resourceId":"v1","resourceName":"vpc-a","stage":"queued",`+
		`"outcome":null,"progress":0,"message":null,"failureReason":null},`+
		`"resource":{"id":"v1","name":"vpc-a","description":null,"cidr":"10.50.0.0/16",`+
		`"datacenter":{"id":"dc-1","code":null,"name":"Kansas"},"status":"creating","failureReason":null,`+
		`"egressIp":null,"activeJobId":"j1","createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z",`+
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
		"an active job":   {keys: `"activeJobId":"j8"`, want: types.StringValue("j8")},
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
