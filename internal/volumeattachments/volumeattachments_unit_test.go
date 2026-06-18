package volumeattachments

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/testutil"
)

const (
	testVMID  = "vm-test-123"
	testVolID = "vol-test-456"
	testJobID = "job-test-001"
)

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
			"volumeType": map[string]any{"id": 1, "name": "SSD", "description": "SSD volume"},
			"datacenter": map[string]any{
				"id": "dc-1", "name": "Chicago", "region": "Central",
				"countryAbbr": "US", "country": "United States",
			},
			"virtualMachineId": attachedVMID, "virtualMachineName": "test-vm",
			"createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z",
		},
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

func TestAttachVolumeHotplugEnabled(t *testing.T) {
	var attachCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"+testVMID):
				testutil.WriteJSONResponse(w, vmResponse(testVMID, 1, "Running"))

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+testVolID+"/attach"):
				attachCalled = true
				body := testutil.ReadRequestBody(r)
				if body["virtualMachineId"] != testVMID {
					t.Errorf("expected virtualMachineId %s in body, got %v", testVMID, body["virtualMachineId"])
				}
				testutil.HandleCreateJobResponse(w, testJobID, "attach started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data": map[string]any{
						"jobs": []map[string]any{
							{"jobId": testJobID, "resourceId": testVolID, "isCompleted": true, "hasFailed": false},
						},
					},
				})

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := AttachVolume(gpcnClient, context.Background(), testVMID, testVolID)
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
	if !attachCalled {
		t.Error("expected attach endpoint to be called")
	}
}

func TestAttachVolumeHotplugDisabledStopsAndStartsVM(t *testing.T) {
	var stopCalled, startCalled, attachCalled bool
	vmStatus := "Running"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"+testVMID):
				testutil.WriteJSONResponse(w, vmResponse(testVMID, 0, vmStatus))

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/stop"):
				stopCalled = true
				vmStatus = "Shutoff"
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/start"):
				startCalled = true
				vmStatus = "Running"
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+testVolID+"/attach"):
				attachCalled = true
				testutil.HandleCreateJobResponse(w, testJobID, "attach started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data": map[string]any{
						"jobs": []map[string]any{
							{"jobId": testJobID, "resourceId": testVolID, "isCompleted": true, "hasFailed": false},
						},
					},
				})

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := AttachVolume(gpcnClient, context.Background(), testVMID, testVolID)
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
	if !stopCalled {
		t.Error("expected VM to be stopped before attach")
	}
	if !attachCalled {
		t.Error("expected attach endpoint to be called")
	}
	if !startCalled {
		t.Error("expected VM to be started after attach")
	}
}

func TestAttachVolumeAlreadyStoppedDoesNotStart(t *testing.T) {
	var stopCalled, startCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"+testVMID):
				// hotplug=0, already Shutoff
				testutil.WriteJSONResponse(w, vmResponse(testVMID, 0, "Shutoff"))

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/stop"):
				stopCalled = true
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/"+testVMID+"/start"):
				startCalled = true
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+testVolID+"/attach"):
				testutil.HandleCreateJobResponse(w, testJobID, "attach started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data": map[string]any{
						"jobs": []map[string]any{
							{"jobId": testJobID, "resourceId": testVolID, "isCompleted": true, "hasFailed": false},
						},
					},
				})

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := AttachVolume(gpcnClient, context.Background(), testVMID, testVolID)
	if err != nil {
		t.Fatalf("AttachVolume failed: %v", err)
	}
	if stopCalled {
		t.Error("expected stop NOT to be called when VM already stopped")
	}
	if startCalled {
		t.Error("expected start NOT to be called when we did not stop the VM")
	}
}

func TestDetachVolumeHotplugEnabled(t *testing.T) {
	var detachCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/virtual-machines/"+testVMID):
				testutil.WriteJSONResponse(w, vmResponse(testVMID, 1, "Running"))

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+testVolID+"/detach"):
				detachCalled = true
				testutil.HandleCreateJobResponse(w, testJobID, "detach started")

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"data": map[string]any{
						"jobs": []map[string]any{
							{"jobId": testJobID, "resourceId": testVolID, "isCompleted": true, "hasFailed": false},
						},
					},
				})

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := DetachVolume(gpcnClient, context.Background(), testVMID, testVolID)
	if err != nil {
		t.Fatalf("DetachVolume failed: %v", err)
	}
	if !detachCalled {
		t.Error("expected detach endpoint to be called")
	}
}
