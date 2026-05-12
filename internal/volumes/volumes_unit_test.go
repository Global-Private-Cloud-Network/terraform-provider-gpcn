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

func newVolumeResponse(id, name string, sizeGb int64, skuId string) *readVolumesResponse {
	resp := &readVolumesResponse{Success: true, Message: "Volume retrieved"}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.SizeGb = sizeGb
	resp.Data.VolumeType.ID = 1
	resp.Data.VolumeType.Name = "SSD"
	resp.Data.VolumeType.Description = "Solid State Drive"
	resp.Data.SkuId = skuId
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
				ComponentCode:  "vol-add-ssd",
				AvailableSizes: sizes,
			}},
		},
	}
}

func TestMapVolumeResponseToModelUnit(t *testing.T) {
	response := newVolumeResponse("volume-123", "test-volume", 256, "sku-uuid-10")
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
		jobID    = "job-123"
		volumeID = "volume-456"
		skuId    = "sku-uuid-10"
	)

	var volumeSizesCalled, createCalled, jobStatusCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes"):
				volumeSizesCalled = true
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{SkuId: skuId, SizeGb: 256},
					{SkuId: "sku-uuid-11", SizeGb: 512},
				}))

			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/volumes/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)

				if req["name"] != "test-volume" {
					t.Errorf("Expected name 'test-volume', got '%v'", req["name"])
				}
				if req["skuId"] != skuId {
					t.Errorf("Expected skuId '%s', got '%v'", skuId, req["skuId"])
				}
				if _, hasSizeGb := req["sizeGb"]; hasSizeGb {
					t.Error("Request body should not contain sizeGb")
				}
				if _, hasVolumeTypeId := req["volumeTypeId"]; hasVolumeTypeId {
					t.Error("Request body should not contain volumeTypeId")
				}
				if _, hasVolumeSizeId := req["volumeSizeId"]; hasVolumeSizeId {
					t.Error("Request body should not contain volumeSizeId")
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
				testutil.WriteJSONResponse(w, newVolumeResponse(volumeID, "test-volume", 256, skuId))

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
				testutil.WriteJSONResponse(w, newVolumeResponse(volumeID, "test-volume", 256, "sku-uuid-10"))
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
		volumeID  = "volume-update-123"
		newSizeGb = int64(512)
		skuId     = "sku-uuid-11"
	)

	var volumeSizesCalled, updateCalled, jobStatusCalled, getCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes"):
				volumeSizesCalled = true
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{SkuId: "sku-uuid-10", SizeGb: 256},
					{SkuId: skuId, SizeGb: newSizeGb},
				}))

			case r.Method == "PUT" && strings.Contains(r.URL.Path, "/volumes/"+volumeID+"/resize"):
				updateCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["newSizeGb"] != float64(newSizeGb) {
					t.Errorf("Expected newSizeGb '%d', got '%v'", newSizeGb, req["newSizeGb"])
				}
				if _, hasSkuId := req["skuId"]; hasSkuId {
					t.Error("Request body should not contain skuId")
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
				resp := newVolumeResponse(volumeID, "test-volume", newSizeGb, skuId)
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

func TestGetVolumeSkuIdMockHTTP(t *testing.T) {
	const (
		componentCode = "vol-add-ssd"
		sizeGb        = int64(256)
		expectedSkuId = "sku-uuid-10"
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{SkuId: expectedSkuId, SizeGb: sizeGb},
					{SkuId: "sku-uuid-11", SizeGb: 512},
					{SkuId: "sku-uuid-12", SizeGb: 1024},
				}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	skuId, err := GetVolumeSkuId(gpcnClient, context.Background(), testDatacenterID, componentCode, sizeGb)
	if err != nil {
		t.Fatalf("GetVolumeSkuId failed: %v", err)
	}
	if skuId != expectedSkuId {
		t.Errorf("Expected SKU ID '%s', got '%s'", expectedSkuId, skuId)
	}
}

func TestGetVolumeSkuIdInvalidSizeMockHTTP(t *testing.T) {
	const (
		componentCode = "vol-add-ssd"
		invalidSizeGb = int64(555)
	)

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/data-centers/") && strings.HasSuffix(r.URL.Path, "/volume-sizes") {
				testutil.WriteJSONResponse(w, newVolumeSizesResponse(testDatacenterID, []volumeSizesDataVolumeTypesAvailableSizesResponse{
					{SkuId: "sku-uuid-10", SizeGb: 256},
					{SkuId: "sku-uuid-11", SizeGb: 512},
					{SkuId: "sku-uuid-12", SizeGb: 1024},
				}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	_, err := GetVolumeSkuId(gpcnClient, context.Background(), testDatacenterID, componentCode, invalidSizeGb)
	if err == nil {
		t.Fatal("Expected error for invalid size, got nil")
	}
	if !strings.Contains(err.Error(), "the specified volume size is not available for this datacenter") {
		t.Errorf("Expected error to contain validation message, got '%s'", err.Error())
	}
}
