package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const (
	gpuPlanTestID           = "gpu-1"
	gpuPlanTestDatacenterID = "dc-1"
	gpuPlanTestSeriesID     = "series-a6000"
	gpuPlanTestSeriesName   = "NVIDIA RTX A6000 Series"
	gpuPlanTestSeriesCode   = "nvidia-rtx_a6000-series"
	gpuPlanTestSkuCode      = "gpu_1x_a6000"
	gpuPlanTestImageName    = "ubuntu-22.04"
	gpuPlanTestSshKeyID     = "key-1"
	gpuPlanTestTimestamp    = "2026-01-02T15:04:05Z"
)

func gpuPlanTestInventoryBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"series": []map[string]any{{
				"id":   gpuPlanTestSeriesID,
				"name": gpuPlanTestSeriesName,
				"code": gpuPlanTestSeriesCode,
				"availability": []map[string]any{{
					"datacenterId":   gpuPlanTestDatacenterID,
					"datacenterName": "Kansas",
					"datacenterCode": "kansas",
					"gpuCounts": []map[string]any{{
						"count": 1,
						"specs": map[string]any{"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 48, "storageGB": 256},
						"availableSkus": []map[string]any{{
							"skuCode":     gpuPlanTestSkuCode,
							"description": "std",
							"specs":       map[string]any{"gpuDescription": "1x A6000", "vcpu": 6, "memoryGiB": 48, "storageGB": 256},
						}},
					}},
				}},
			}},
		},
	}
}

func gpuPlanTestReadBody(name string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":        gpuPlanTestID,
			"name":      name,
			"createdAt": gpuPlanTestTimestamp,
			"updatedAt": gpuPlanTestTimestamp,
			"status":    "Running",
			"ip":        "10.0.0.1",
			"configuration": map[string]any{
				"name":     gpuPlanTestSeriesName,
				"code":     gpuPlanTestSeriesCode,
				"skuCode":  gpuPlanTestSkuCode,
				"gpuCount": 1,
				"cpu":      6,
				"ram":      48,
				"disk":     256,
			},
			"datacenter": map[string]any{
				"id":          gpuPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
			"sshKeyId": gpuPlanTestSshKeyID,
			"image":    gpuPlanTestImageName,
		},
	}
}

// startGPUPlanMockServer serves the GPU endpoints a rename needs. The handler keeps
// the name from the last create or update, so the read after an apply agrees with
// the configuration and leaves the refresh plan empty. The returned function renames
// the GPU out of band, which is how a test creates drift.
func startGPUPlanMockServer(t *testing.T) (*httptest.Server, func(string)) {
	t.Helper()

	var mu sync.Mutex
	name := ""

	gpuPath := "/v1/resource/gpu/" + gpuPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/gpu/inventory":
			testutil.WriteJSONResponse(w, gpuPlanTestInventoryBody())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/gpu/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", gpuPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", gpuPlanTestID, true)
		case r.Method == http.MethodPut && r.URL.Path == gpuPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == gpuPath:
			mu.Lock()
			current := name
			mu.Unlock()
			testutil.WriteJSONResponse(w, gpuPlanTestReadBody(current))
		case r.Method == http.MethodDelete && r.URL.Path == gpuPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	setName := func(newName string) {
		mu.Lock()
		name = newName
		mu.Unlock()
	}

	return server, setName
}

func gpuPlanTestConfig(host, seriesField, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_gpu" "test" {
  name          = %q
  datacenter_id = %q
  %s
  gpu_count  = 1
  image_name = %q
  initial_auth = {
    ssh_key_id = %q
  }
}
`, host, name, gpuPlanTestDatacenterID, seriesField, gpuPlanTestImageName, gpuPlanTestSshKeyID)
}

func TestGPUResourcePlanRenameWithSeriesCode(t *testing.T) {
	t.Parallel()
	server, _ := startGPUPlanMockServer(t)

	seriesField := fmt.Sprintf("series_code = %q\n  sku_code    = %q", gpuPlanTestSeriesCode, gpuPlanTestSkuCode)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: gpuPlanTestConfig(server.URL, seriesField, "gpu-plan-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "gpu-plan-a"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_code", gpuPlanTestSeriesCode),
					resource.TestCheckResourceAttr(gpcnGPUTest, "sku_code", gpuPlanTestSkuCode),
				),
			},
			{
				Config: gpuPlanTestConfig(server.URL, seriesField, "gpu-plan-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "gpu-plan-b"),
				),
			},
		},
	})
}

func TestGPUResourcePlanRenameWithSeriesName(t *testing.T) {
	t.Parallel()
	server, _ := startGPUPlanMockServer(t)

	seriesField := fmt.Sprintf("series_name = %q", gpuPlanTestSeriesName)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: gpuPlanTestConfig(server.URL, seriesField, "gpu-plan-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "gpu-plan-a"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_name", gpuPlanTestSeriesName),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_code", gpuPlanTestSeriesCode),
				),
			},
			{
				Config: gpuPlanTestConfig(server.URL, seriesField, "gpu-plan-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "gpu-plan-b"),
				),
			},
		},
	})
}

func TestGPUResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	server, setName := startGPUPlanMockServer(t)

	seriesField := fmt.Sprintf("series_code = %q\n  sku_code    = %q", gpuPlanTestSeriesCode, gpuPlanTestSkuCode)
	config := gpuPlanTestConfig(server.URL, seriesField, "gpu-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				PreConfig: func() { setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "gpu-plan-a"),
				),
			},
		},
	})
}
