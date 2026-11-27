package virtualmachines

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

// busyOnce records one function call whose first send finds a holder.
type busyOnce struct {
	mu     sync.Mutex
	events []string
	bodies []string
}

// setupBusyOnce answers the named request busy once and then with accepted. Every
// job ends with success. Every other request goes to other.
func setupBusyOnce(t *testing.T, method, path string, accepted, other http.HandlerFunc) (*client.GpcnClient, *busyOnce) {
	t.Helper()

	previous := BusyRetryInterval
	BusyRetryInterval = time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	recorded := &busyOnce{}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			recorded.mu.Lock()
			defer recorded.mu.Unlock()
			switch {
			case r.Method == method && r.URL.Path == path:
				raw, _ := io.ReadAll(r.Body)
				recorded.bodies = append(recorded.bodies, string(raw))
				recorded.events = append(recorded.events, "send")
				if len(recorded.bodies) == 1 {
					testutil.HandleBusyResponse(w, "holder-job")
					return
				}
				accepted(w, r)
			case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
				ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
				jobID, _ := ids[0].(string)
				recorded.events = append(recorded.events, "poll "+jobID)
				testutil.HandleJobResponse(w, jobID, "", true)
			case other != nil:
				recorded.events = append(recorded.events, r.Method+" "+r.URL.Path)
				other(w, r)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	return gpcnClient, recorded
}

func (b *busyOnce) check(t *testing.T, wantBody string, wantEvents ...string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bodies) != 2 {
		t.Fatalf("sends = %d, want 2", len(b.bodies))
	}
	if b.bodies[1] != wantBody {
		t.Errorf("second body = %q, want %q", b.bodies[1], wantBody)
	}
	if !slices.Equal(b.events, wantEvents) {
		t.Errorf("events = %q, want %q", b.events, wantEvents)
	}
}

func acceptOwnJob(w http.ResponseWriter, _ *http.Request) {
	testutil.HandleCreateJobResponse(w, "own-job")
}

func acceptSync(w http.ResponseWriter, _ *http.Request) {
	testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{}})
}

func TestUpdateVirtualMachineWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vm-1", acceptSync, nil)

	if err := UpdateVirtualMachine(gpcnClient, context.Background(), "vm-1", map[string]any{"name": "renamed"}); err != nil {
		t.Fatalf("UpdateVirtualMachine = %v, want nil", err)
	}
	recorded.check(t, `{"name":"renamed"}`, "send", "poll holder-job", "send")
}

func TestUpdateVirtualMachineSizeWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vm-1/size", acceptOwnJob, nil)

	if err := UpdateVirtualMachineSize(gpcnClient, context.Background(), "vm-1", "sku-2"); err != nil {
		t.Fatalf("UpdateVirtualMachineSize = %v, want nil", err)
	}
	recorded.check(t, `{"skuId":"sku-2"}`, "send", "poll holder-job", "send", "poll own-job")
}

func TestStopVirtualMachineWaitsOutBusyHolder(t *testing.T) {
	useFastVMStatusPollInterval(t)
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, BASE_URL_V1+"vm-1/stop", acceptSync, func(w http.ResponseWriter, _ *http.Request) {
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": "vm-1", "status": "Shutoff"}})
	})

	if err := StopVirtualMachine(gpcnClient, context.Background(), "vm-1"); err != nil {
		t.Fatalf("StopVirtualMachine = %v, want nil", err)
	}
	recorded.check(t, "", "send", "poll holder-job", "send", "GET "+BASE_URL_V1+"vm-1")
}

func TestStartVirtualMachineWaitsOutBusyHolder(t *testing.T) {
	useFastVMStatusPollInterval(t)
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, BASE_URL_V1+"vm-1/start", acceptSync, func(w http.ResponseWriter, _ *http.Request) {
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": "vm-1", "status": "Running"}})
	})

	if err := StartVirtualMachine(gpcnClient, context.Background(), "vm-1"); err != nil {
		t.Fatalf("StartVirtualMachine = %v, want nil", err)
	}
	recorded.check(t, "", "send", "poll holder-job", "send", "GET "+BASE_URL_V1+"vm-1")
}
