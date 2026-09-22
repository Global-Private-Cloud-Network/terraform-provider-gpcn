package vpcs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

const vpcNotActiveBody = `{"success":false,` +
	`"message":"The VPC is not active (status: creating); this operation needs an active VPC.",` +
	`"error":{"code":"VPC_NOT_ACTIVE","statusCode":409,"details":null}}`

// vpcDeleteMock answers the first delete with the refusal a creating VPC
// raises, and the second with the teardown job.
type vpcDeleteMock struct {
	mutex sync.Mutex
	// refusals is how many DELETEs answer VPC_NOT_ACTIVE before one is taken.
	refusals    int
	status      string
	activeJobID any
	deletes     int
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
			status := m.status
			activeJobID := m.activeJobID
			m.mutex.Unlock()

			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id":          vpcUnitTestID,
					"name":        "vpc-creating",
					"cidr":        "10.50.0.0/16",
					"status":      status,
					"activeJobId": activeJobID,
					"createdAt":   vpcUnitTestCreated,
					"updatedAt":   vpcUnitTestUpdated,
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
