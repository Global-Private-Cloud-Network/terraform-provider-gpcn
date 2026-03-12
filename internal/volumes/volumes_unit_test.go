package volumes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const testDatacenterID = "datacenter-123"

func createTestVolumeModel(name, volumeType string, sizeGb int64) ResourceModel {
	return ResourceModel{
		Name:         types.StringValue(name),
		DatacenterId: types.StringValue(testDatacenterID),
		VolumeType:   types.StringValue(volumeType),
		SizeGb:       types.Int64Value(sizeGb),
	}
}

func newVolumeResponse(id, name string, sizeGb, volumeSizeID int64) *readVolumesResponse {
	resp := &readVolumesResponse{Success: true, Message: "Volume retrieved"}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.SizeGb = sizeGb
	resp.Data.VolumeType.ID = 1
	resp.Data.VolumeType.Name = "SSD"
	resp.Data.VolumeType.Description = "Solid State Drive"
	resp.Data.VolumeSizeId = volumeSizeID
	resp.Data.Datacenter.ID = testDatacenterID
	resp.Data.Datacenter.Name = "US-East-1"
	resp.Data.Datacenter.Region = "East"
	resp.Data.Datacenter.Country = "US"
	resp.Data.VirtualMachineId = ""
	resp.Data.VirtualMachineName = ""
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	return resp
}

func newVolumeSizesResponse(datacenterID string, sizes []volumeSizesDataVolumeTypesAvailableSizesResponse) volumeSizesResponse {
	return volumeSizesResponse{
		Success: true,
		Message: "Volume sizes retrieved",
		Data: volumeSizesDataResponse{
			DatacenterId: datacenterID,
			VolumeTypes: []volumeSizesDataVolumeTypesResponse{{
				ID:             1,
				Name:           "SSD",
				Description:    "Solid State Drive",
				AvailableSizes: sizes,
			}},
		},
	}
}

func TestMapVolumeResponseToModelUnit(t *testing.T) {
	response := newVolumeResponse("volume-123", "test-volume", 256, 10)
	model := createTestVolumeModel("test-volume", "SSD", 256)

	result := MapVolumeResponseToModel(context.Background(), response, model)

	if result.ID.ValueString() != "volume-123" {
		t.Errorf("Expected ID 'volume-123', got '%s'", result.ID.ValueString())
	}
	if result.VolumeTypeId.ValueInt64() != 1 {
		t.Errorf("Expected VolumeTypeId 1, got %d", result.VolumeTypeId.ValueInt64())
	}
	if result.CreatedTime.IsNull() || result.CreatedTime.ValueString() == "unknown" {
		t.Errorf("Expected CreatedTime to be set, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.IsNull() || result.LastUpdated.ValueString() == "unknown" {
		t.Errorf("Expected LastUpdated to be set, got '%s'", result.LastUpdated.ValueString())
	}
	if result.Location.IsNull() {
		t.Error("Expected Location to be set")
	}
}

func TestCreateVolumeMockHTTP(t *testing.T) {
	const (
		jobID        = "job-123"
		volumeID     = "volume-456"
		volumeSizeID = int64(10)
	)

	var volumeSizesCalled, createCalled, jobStatusCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes"):
				volumeSizesCalled = true
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{ID: volumeSizeID, SizeGb: 256},
					{ID: 11, SizeGb: 512},
				}))

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/volumes/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)

				if req["name"] != "test-volume" {
					t.Errorf("Expected name 'test-volume', got '%v'", req["name"])
				}
				if int64(req["sizeGb"].(float64)) != 256 {
					t.Errorf("Expected sizeGb 256, got '%v'", req["sizeGb"])
				}

				testutil.WriteJSONResponse(w, client.JobStatusSingularResponse{
					Success: true,
					Message: "Volume creation job started",
					Data:    client.JobResponse{JobID: jobID},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, jobID, volumeID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID):
				getCalled = true
				testutil.WriteJSONResponse(w, newVolumeResponse(volumeID, "test-volume", 256, volumeSizeID))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := CreateVolume(gpcnClient, context.Background(), createTestVolumeModel("test-volume", "SSD", 256))
	if err != nil {
		t.Fatalf("CreateVolume failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != volumeID {
		t.Errorf("Expected volume ID '%s', got '%s'", volumeID, response.Data.ID)
	}
	if !volumeSizesCalled {
		t.Error("Expected volume sizes endpoint to be called")
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get volume endpoint to be called")
	}
}

func TestGetVolumeMockHTTP(t *testing.T) {
	const volumeID = "volume-789"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID) {
				testutil.WriteJSONResponse(w, newVolumeResponse(volumeID, "test-volume", 256, 10))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetVolume(gpcnClient, context.Background(), volumeID)
	if err != nil {
		t.Fatalf("GetVolume failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != volumeID {
		t.Errorf("Expected volume ID '%s', got '%s'", volumeID, response.Data.ID)
	}
}

func TestUpdateVolumeMockHTTP(t *testing.T) {
	const (
		volumeID     = "volume-update-123"
		newSizeGb    = int64(512)
		volumeSizeID = int64(11)
	)

	var volumeSizesCalled, updateCalled, jobStatusCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes"):
				volumeSizesCalled = true
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{ID: 10, SizeGb: 256},
					{ID: volumeSizeID, SizeGb: newSizeGb},
				}))

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+volumeID+"/resize"):
				updateCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if int64(req["newSizeGb"].(float64)) != newSizeGb {
					t.Errorf("Expected newSizeGb %d, got '%v'", newSizeGb, req["newSizeGb"])
				}
				testutil.WriteJSONResponse(w, client.JobStatusSingularResponse{
					Success: true,
					Message: "Volume resize job started",
					Data:    client.JobResponse{JobID: "job-456"},
				})

			case r.Method == "POST" && strings.Contains(r.URL.Path, "/jobs"):
				jobStatusCalled = true
				testutil.HandleJobResponse(w, "job-456", volumeID, true)

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"+volumeID):
				getCalled = true
				resp := newVolumeResponse(volumeID, "test-volume", newSizeGb, volumeSizeID)
				resp.Data.CreatedAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
				testutil.WriteJSONResponse(w, resp)

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createTestVolumeModel("test-volume", "SSD", newSizeGb)
	model.ID = types.StringValue(volumeID)

	response, err := UpdateVolume(gpcnClient, context.Background(), volumeID, model)
	if err != nil {
		t.Fatalf("UpdateVolume failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.SizeGb != newSizeGb {
		t.Errorf("Expected volume size %d, got %d", newSizeGb, response.Data.SizeGb)
	}
	if !volumeSizesCalled {
		t.Error("Expected volume sizes endpoint to be called")
	}
	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
	if !jobStatusCalled {
		t.Error("Expected job status endpoint to be called")
	}
	if !getCalled {
		t.Error("Expected get volume endpoint to be called")
	}
}

func TestGetVolumeSizeIdMockHTTP(t *testing.T) {
	const (
		volumeTypeID         = int64(1)
		sizeGb               = int64(256)
		expectedVolumeSizeID = int64(10)
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{ID: expectedVolumeSizeID, SizeGb: sizeGb},
					{ID: 11, SizeGb: 512},
					{ID: 12, SizeGb: 1024},
				}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	volumeSizeID, err := GetVolumeSizeId(gpcnClient, context.Background(), testDatacenterID, volumeTypeID, sizeGb)
	if err != nil {
		t.Fatalf("GetVolumeSizeId failed: %v", err)
	}
	if volumeSizeID != expectedVolumeSizeID {
		t.Errorf("Expected volume size ID %d, got %d", expectedVolumeSizeID, volumeSizeID)
	}
}

func TestGetVolumeSizeIdInvalidSizeMockHTTP(t *testing.T) {
	const (
		volumeTypeID  = int64(1)
		invalidSizeGb = int64(555)
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{ID: 10, SizeGb: 256},
					{ID: 11, SizeGb: 512},
					{ID: 12, SizeGb: 1024},
				}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	_, err := GetVolumeSizeId(gpcnClient, context.Background(), testDatacenterID, volumeTypeID, invalidSizeGb)
	if err == nil {
		t.Fatal("Expected error for invalid size, got nil")
	}
	if !strings.Contains(err.Error(), "the specified volume size is not available for this datacenter") {
		t.Errorf("Expected error to contain validation message, got '%s'", err.Error())
	}
}
