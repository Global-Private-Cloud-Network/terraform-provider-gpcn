package volumeattachments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

const (
	testVMID  = "vm-test-123"
	testVolID = "vol-test-456"
	testJobID = "job-test-001"
)

// backendStatusRefusal is the byte-exact sentence GPCN answers with 400 when the
// target machine is not settled. DEV src/services/volumes.service.ts:108-116 builds
// it, and the single quotes around the status are part of it.
const backendStatusRefusal = "Cannot attach a volume while the VM is in status 'Provisioning'. " +
	"The VM must be Running, Stopped, Shutoff."

func vmResponse(id string, hotplug int, status string) map[string]any {
	return map[string]any{
		"success": true, "message": "VM retrieved",
		"data": map[string]any{
			"id": id, "name": "test-vm", "status": status,
			"networkHotplug": hotplug,
			"createdAt":      "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z",
			"configuration": map[string]any{
				"id": 1, "name": "G-Micro-1", "code": "G-Micro-1",
				"categoryCode": "general", "skuId": "sku-1", "skuCode": "general-G-Micro-1",
				"cpu": 1, "ram": 2, "disk": 20,
			},
			"image": "Alma Linux 8.x", "username": "admin",
			"datacenter": map[string]any{
				"id": "dc-1", "name": "Chicago", "region": "Central",
				"countryAbbr": "US", "country": "United States",
			},
		},
	}
}

func volumeResponse(volID, attachedVMID string) map[string]any {
	return map[string]any{
		"success": true, "message": "Volume retrieved",
		"data": map[string]any{
			"id": volID, "name": "test-vol", "sizeGb": 100,
			"skuId":      "sku-vol-1",
			"volumeType": map[string]any{"name": "SSD", "description": "SSD volume"},
			"datacenter": map[string]any{
				"id": "dc-1", "name": "Chicago", "region": "Central",
				"countryAbbr": "US", "country": "United States",
			},
			"virtualMachineId": attachedVMID, "virtualMachineName": "test-vm",
			"createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z",
		},
	}
}

// requestRecorder keeps the method and path of every request the mock server saw.
// The handler runs on the server goroutine, so the mutex guards the slice.
type requestRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *requestRecorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, req.Method+" "+req.URL.Path)
}

func (r *requestRecorder) saw(method, pathSuffix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		if strings.HasPrefix(line, method+" ") && strings.HasSuffix(line, pathSuffix) {
			return true
		}
	}
	return false
}

func (r *requestRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, ", ")
}

// volumeOperationServer answers the named volume verb and records every request.
// The machine read, stop and start arms stay available, so a recorded stop proves a
// deliberate call. The stop and start arms answer 500 to keep a failure fast.
func volumeOperationServer(t *testing.T, volumeAction string) (*client.GpcnClient, *requestRecorder) {
	t.Helper()
	recorder := &requestRecorder{}

	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			recorder.record(r)
			switch {
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"+testVMID):
				testutil.WriteJSONResponse(w, vmResponse(testVMID, 0, "Running"))

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/stop"):
				w.WriteHeader(http.StatusInternalServerError)

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/start"):
				w.WriteHeader(http.StatusInternalServerError)

			case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/volumes/"+testVolID+"/"+volumeAction):
				if volumeAction == "attach" {
					body := testutil.ReadRequestBody(r)
					if body["virtualMachineId"] != testVMID {
						t.Errorf("expected virtualMachineId %s in the attach body, got %v", testVMID, body["virtualMachineId"])
					}
				}
				testutil.HandleCreateJobResponse(w, testJobID, volumeAction+" started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.HandleJobResponse(w, testJobID, testVolID, true)

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	return gpcnClient, recorder
}

func assertNoStopOrStart(t *testing.T, recorder *requestRecorder) {
	t.Helper()
	if recorder.saw("POST", "/"+testVMID+"/stop") {
		t.Errorf("the volume operation stopped the virtual machine; requests: %s", recorder)
	}
	if recorder.saw("POST", "/"+testVMID+"/start") {
		t.Errorf("the volume operation started the virtual machine; requests: %s", recorder)
	}
}

func TestGetAttachedVMIdAttached(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+testVolID) {
				testutil.WriteJSONResponse(w, volumeResponse(testVolID, testVMID))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	vmId, err := GetAttachedVMId(gpcnClient, context.Background(), testVolID)
	if err != nil {
		t.Fatalf("GetAttachedVMId failed: %v", err)
	}
	if vmId != testVMID {
		t.Errorf("expected VM ID %s, got %s", testVMID, vmId)
	}
}

func TestGetAttachedVMIdNotAttached(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+testVolID) {
				testutil.WriteJSONResponse(w, volumeResponse(testVolID, ""))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	vmId, err := GetAttachedVMId(gpcnClient, context.Background(), testVolID)
	if err != nil {
		t.Fatalf("GetAttachedVMId failed: %v", err)
	}
	if vmId != "" {
		t.Errorf("expected empty VM ID for detached volume, got %s", vmId)
	}
}

func TestAttachVolumeDoesNotStopVMMockHTTP(t *testing.T) {
	gpcnClient, recorder := volumeOperationServer(t, "attach")

	err := AttachVolume(gpcnClient, context.Background(), testVMID, testVolID)

	assertNoStopOrStart(t, recorder)
	if !recorder.saw("PUT", "/volumes/"+testVolID+"/attach") {
		t.Errorf("expected the attach verb to be called; requests: %s", recorder)
	}
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
}

func TestDetachVolumeDoesNotStopVMMockHTTP(t *testing.T) {
	gpcnClient, recorder := volumeOperationServer(t, "detach")

	err := DetachVolume(gpcnClient, context.Background(), testVolID)

	assertNoStopOrStart(t, recorder)
	if !recorder.saw("PUT", "/volumes/"+testVolID+"/detach") {
		t.Errorf("expected the detach verb to be called; requests: %s", recorder)
	}
	if err != nil {
		t.Fatalf("DetachVolume failed: %v", err)
	}
}

func TestAttachSurfacesBackendStatusRefusalMockHTTP(t *testing.T) {
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/volumes/"+testVolID+"/attach") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"message": backendStatusRefusal,
					"error": map[string]any{
						"code":       "Validation Error",
						"statusCode": 400,
						"details":    nil,
					},
				})
				return
			}
			testutil.LogUnexpectedRequest(t, w, r)
		},
	})

	err := AttachVolume(gpcnClient, context.Background(), testVMID, testVolID)
	if err == nil {
		t.Fatal("expected AttachVolume to report the refusal")
	}

	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected a *client.HTTPError in the chain, got %T: %v", err, err)
	}
	want := "HTTP 400 (Validation Error): " + backendStatusRefusal
	if got := httpErr.Error(); got != want {
		t.Errorf("HTTPError.Error() =\n%q\nwant\n%q", got, want)
	}
	// net/http wraps the transport error in *url.Error, so the request line is the
	// only text before the rendered HTTPError. A provider wrapper adds more.
	if !strings.HasPrefix(err.Error(), `Put "`) {
		t.Errorf("AttachVolume error =\n%q\nwant the url.Error request line as the only prefix", err.Error())
	}
	if code := client.ErrorCode(err); code != "Validation Error" {
		t.Errorf("client.ErrorCode = %q, want %q", code, "Validation Error")
	}
}

// TestDetachVolumeNotFoundStaysNotFound guards the classification Delete depends on.
// Delete reads a not-found detach as an attachment that is already gone.
func TestDetachVolumeNotFoundStaysNotFound(t *testing.T) {
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/volumes/"+testVolID+"/detach") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			testutil.LogUnexpectedRequest(t, w, r)
		},
	})

	err := DetachVolume(gpcnClient, context.Background(), testVolID)
	if err == nil {
		t.Fatal("expected DetachVolume to fail when the volume is gone")
	}
	if !client.IsNotFound(err) {
		t.Errorf("expected client.IsNotFound to be true, got false for error: %v", err)
	}
}
