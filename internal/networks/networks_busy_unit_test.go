package networks

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

var interfacesPath = VIRTUAL_MACHINES_BASE_URL_V1 + "vm-1/network-interfaces"

// interfacesOnSegments answers each interface read with the next segment of
// the list. An empty segment lists no interface.
func interfacesOnSegments(segments ...string) http.HandlerFunc {
	reads := 0
	return func(w http.ResponseWriter, _ *http.Request) {
		segment := segments[min(reads, len(segments)-1)]
		reads++
		data := []map[string]any{}
		if segment != "" {
			data = append(data, map[string]any{"id": "nic-2", "networkInterface": 1, "world": NicWorldL2, "l2SegmentId": segment, "operationState": "attaching"})
		}
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": data})
	}
}

func TestAddL2SegmentInterfaceWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, interfacesPath, acceptOwnJob, interfacesOnSegments(""))

	if err := AddL2SegmentInterface(gpcnClient, context.Background(), "vm-1", "seg-1"); err != nil {
		t.Fatalf("AddL2SegmentInterface = %v, want nil", err)
	}
	read := "GET " + interfacesPath
	recorded.check(t, `{"l2SegmentId":"seg-1"}`, read, "send", "poll holder-job", read, "send", "poll own-job")
}

func TestAddL2SegmentInterfaceLeavesASegmentTheHolderAttached(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, interfacesPath, acceptOwnJob, interfacesOnSegments("", "seg-1"))

	if err := AddL2SegmentInterface(gpcnClient, context.Background(), "vm-1", "seg-1"); err != nil {
		t.Fatalf("AddL2SegmentInterface = %v, want nil", err)
	}
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	read := "GET " + interfacesPath
	if want := []string{read, "send", "poll holder-job", read}; !slices.Equal(recorded.events, want) {
		t.Errorf("events = %q, want %q", recorded.events, want)
	}
}

// DoWithRetry sends the attach again after the 502. The API accepted the
// first POST, so the second meets the job of this attach as the holder.
func TestAddL2SegmentInterfaceSkipsItsOwnAcceptedAttach(t *testing.T) {
	previous := BusyRetryInterval
	BusyRetryInterval = time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	var mu sync.Mutex
	var events []string
	posts := 0
	listInterfaces := interfacesOnSegments("", "seg-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == interfacesPath:
			events = append(events, "read")
			listInterfaces(w, r)
		case r.Method == http.MethodPost && r.URL.Path == interfacesPath:
			posts++
			events = append(events, "send")
			if posts == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			testutil.HandleBusyResponse(w, "own-job")
		case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
			ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
			jobID, _ := ids[0].(string)
			events = append(events, "poll "+jobID)
			testutil.HandleJobResponse(w, jobID, "", true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := client.DefaultConfig(server.URL, "test-key")
	cfg.MaxRetries = 1
	cfg.InitialRetryDelay = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if err := AddL2SegmentInterface(gpcnClient, context.Background(), "vm-1", "seg-1"); err != nil {
		t.Fatalf("AddL2SegmentInterface = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 2 {
		t.Errorf("POSTs = %d, want 2", posts)
	}
	if want := []string{"read", "send", "send", "poll own-job", "read"}; !slices.Equal(events, want) {
		t.Errorf("events = %q, want %q", events, want)
	}
}

func TestRemoveNetworkInterfaceWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodDelete, VIRTUAL_MACHINES_BASE_URL_V1+"vm-1/network-interfaces/nic-1", acceptOwnJob, nil)

	if err := RemoveNetworkInterface(gpcnClient, context.Background(), "vm-1", "nic-1"); err != nil {
		t.Fatalf("RemoveNetworkInterface = %v, want nil", err)
	}
	recorded.check(t, "", "send", "poll holder-job", "send", "poll own-job")
}
