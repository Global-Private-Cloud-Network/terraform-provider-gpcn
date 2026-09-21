package vpcs

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

const vpcNotActiveBody = `{"success":false,` +
	`"message":"The VPC is not active (status: creating); this operation needs an active VPC.",` +
	`"error":{"code":"VPC_NOT_ACTIVE","statusCode":409,"details":null}}`

// vpcDeleteMock answers the first delete with the refusal a creating VPC
// raises, and the second with the teardown job.
type vpcDeleteMock struct {
	mutex       sync.Mutex
	status      string
	activeJobID any
	deletes     int
}

func (m *vpcDeleteMock) handler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			m.mutex.Lock()
			m.deletes++
			refuse := m.deletes == 1
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

	mock := &vpcDeleteMock{status: VPC_STATUS_CREATING, activeJobID: "job-create"}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	if err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID); err != nil {
		t.Fatalf("Expected the retried delete to succeed, got %v", err)
	}
	if mock.deletes != 2 {
		t.Errorf("DELETE count = %d, want 2", mock.deletes)
	}
}

// Nothing the provider can wait for clears a refusal on an active VPC, so the
// refusal reaches the reader.
func TestVpcDeleteDoesNotRetryWhenNoJobOwnsTheVpc(t *testing.T) {
	t.Parallel()

	mock := &vpcDeleteMock{status: VPC_STATUS_ACTIVE, activeJobID: nil}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: mock.handler(t)})

	err := DeleteVpc(gpcnClient, context.Background(), vpcUnitTestID)
	if err == nil {
		t.Fatalf("Expected the refusal to reach the caller")
	}
	if !client.HasErrorCode(err, ERROR_CODE_VPC_NOT_ACTIVE) {
		t.Errorf("Expected a VPC_NOT_ACTIVE error, got %v", err)
	}
	if mock.deletes != 1 {
		t.Errorf("DELETE count = %d, want 1", mock.deletes)
	}
}
