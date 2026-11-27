package volumes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestUpdateVolumeWaitsOutBusyHolder(t *testing.T) {
	sizesPath := DATA_CENTERS_BASE_URL_V1 + testDatacenterID + "/volume-sizes"
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vol-1/resize", acceptOwnJob, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sizesPath {
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"volumeTypes": []map[string]any{{
				"componentCode":  "vol-add-ssd",
				"availableSizes": []map[string]any{{"skuId": "sku-1", "sizeGb": 200}},
			}}}})
			return
		}
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": "vol-1"}})
	})

	if _, err := UpdateVolume(gpcnClient, context.Background(), "vol-1", createTestVolumeModel("vol", "SSD", 200)); err != nil {
		t.Fatalf("UpdateVolume = %v, want nil", err)
	}
	recorded.check(t, `{"newSizeGb":200}`, "GET "+sizesPath, "send", "poll holder-job", "send", "poll own-job", "GET "+BASE_URL_V1+"vol-1")
}

func TestMoveVolumeWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vol-1", acceptSync, nil)

	if err := MoveVolume(gpcnClient, context.Background(), "vol-1", "group-1"); err != nil {
		t.Fatalf("MoveVolume = %v, want nil", err)
	}
	recorded.check(t, `{"resourceGroupId":"group-1"}`, "send", "poll holder-job", "send")
}

func TestDeleteVolumeWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodDelete, BASE_URL_V1+"vol-1", acceptOwnJob, func(w http.ResponseWriter, _ *http.Request) {
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": "vol-1"}})
	})

	if err := DeleteVolume(gpcnClient, context.Background(), "vol-1"); err != nil {
		t.Fatalf("DeleteVolume = %v, want nil", err)
	}
	recorded.check(t, "", "GET "+BASE_URL_V1+"vol-1", "send", "poll holder-job", "send", "poll own-job")
}

func TestAddVolumeToVirtualMachineWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vol-1/attach", acceptOwnJob, nil)

	if err := AddVolumeToVirtualMachine(gpcnClient, context.Background(), "vm-1", "vol-1"); err != nil {
		t.Fatalf("AddVolumeToVirtualMachine = %v, want nil", err)
	}
	recorded.check(t, `{"virtualMachineId":"vm-1"}`, "send", "poll holder-job", "send", "poll own-job")
}

// volumeOnMachines answers each volume read with the next machine of the list.
func volumeOnMachines(machines ...string) http.HandlerFunc {
	reads := 0
	return func(w http.ResponseWriter, _ *http.Request) {
		machine := machines[min(reads, len(machines)-1)]
		reads++
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{
			"id": "vol-1", "virtualMachineId": machine,
		}})
	}
}

func TestRemoveVolumeFromVirtualMachineWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vol-1/detach", acceptOwnJob, volumeOnMachines("vm-1"))

	if err := RemoveVolumeFromVirtualMachine(gpcnClient, context.Background(), "vol-1", "vm-1"); err != nil {
		t.Fatalf("RemoveVolumeFromVirtualMachine = %v, want nil", err)
	}
	read := "GET " + BASE_URL_V1 + "vol-1"
	recorded.check(t, "", read, "send", "poll holder-job", read, "send", "poll own-job")
}

// checkOneDetach pins a detach that the second volume read stopped.
func checkOneDetach(t *testing.T, machineAfterTheWait string) {
	t.Helper()
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, BASE_URL_V1+"vol-1/detach", acceptOwnJob, volumeOnMachines("vm-1", machineAfterTheWait))

	if err := RemoveVolumeFromVirtualMachine(gpcnClient, context.Background(), "vol-1", "vm-1"); err != nil {
		t.Fatalf("RemoveVolumeFromVirtualMachine = %v, want nil", err)
	}
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	read := "GET " + BASE_URL_V1 + "vol-1"
	if want := []string{read, "send", "poll holder-job", read}; !slices.Equal(recorded.events, want) {
		t.Errorf("events = %q, want %q", recorded.events, want)
	}
}

func TestRemoveVolumeFromVirtualMachineLeavesAVolumeThatMoved(t *testing.T) {
	checkOneDetach(t, "vm-2")
}

func TestRemoveVolumeFromVirtualMachineSkipsADetachedVolume(t *testing.T) {
	checkOneDetach(t, "")
}

// The holder takes 2.5s of the 4s timeout, so 1.5s of the wait remains. The
// second null read of the resize job starts about 2.5s after the accept.
func TestUpdateVolumeKeepsItsOwnTimeoutAfterAWait(t *testing.T) {
	const (
		pollingTimeout = 4 * time.Second
		holderEnds     = 2500 * time.Millisecond
		firstReadEnds  = 1500 * time.Millisecond
	)
	previous := BusyRetryInterval
	BusyRetryInterval = time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	var mu sync.Mutex
	var sentAt, acceptedAt time.Time
	ownReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == DATA_CENTERS_BASE_URL_V1+testDatacenterID+"/volume-sizes":
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"volumeTypes": []map[string]any{{
				"componentCode":  "vol-add-ssd",
				"availableSizes": []map[string]any{{"skuId": "sku-1", "sizeGb": 200}},
			}}}})
		case r.Method == http.MethodGet && r.URL.Path == BASE_URL_V1+"vol-1":
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": "vol-1"}})
		case r.Method == http.MethodPut && r.URL.Path == BASE_URL_V1+"vol-1/resize":
			if sentAt.IsZero() {
				sentAt = time.Now()
				testutil.HandleBusyResponse(w, "holder-job")
				return
			}
			acceptedAt = time.Now()
			acceptOwnJob(w, r)
		case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
			ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
			if jobID, _ := ids[0].(string); jobID == "holder-job" {
				time.Sleep(time.Until(sentAt.Add(holderEnds)))
				testutil.HandleJobResponse(w, jobID, "", true)
				return
			}
			ownReads++
			if ownReads == 1 {
				time.Sleep(time.Until(acceptedAt.Add(firstReadEnds)))
			}
			testutil.HandleJobResponse(w, "own-job", "", ownReads > 2)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 0
	cfg.PollingTimeout = pollingTimeout
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if _, err := UpdateVolume(gpcnClient, context.Background(), "vol-1", createTestVolumeModel("vol", "SSD", 200)); err != nil {
		t.Fatalf("UpdateVolume = %v, want nil", err)
	}
}
