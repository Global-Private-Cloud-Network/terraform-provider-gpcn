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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gpuPlanTestID           = "gpu-1"
	gpuPlanTestDatacenterID = "dc-1"
	gpuPlanTestSeriesID     = "series-a6000"
	gpuPlanTestSeriesName   = "NVIDIA RTX A6000 Series"
	gpuPlanTestSeriesCode   = "nvidia-rtx_a6000-series"
	gpuPlanTestSkuCode      = "gpu_1x_a6000"
	gpuPlanTestImageName    = "ubuntu-22.04"
	// The API returns a longer image name than the configuration, so the fixture
	// proves that Read keeps the configured value.
	gpuPlanTestAPIImageName = "Ubuntu 22.04 LTS (x86_64)"
	gpuPlanTestSshKeyID     = "key-1"
	gpuPlanTestTimestamp    = "2026-01-02T15:04:05Z"
)

const (
	gpuCatalogTestSeriesID   = "series-a100"
	gpuCatalogTestSeriesName = "NVIDIA A100 Series"
	gpuCatalogTestSeriesCode = "nvidia-a100-series"
	gpuCatalogTestSkuCode    = "gpu_1x_a100"
)

// gpuPlanTestInventoryBody offers both test series in the same datacenter at the
// same GPU count. The series a configuration names is then the only thing that
// decides which series ID an order carries.
func gpuPlanTestInventoryBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"series": []map[string]any{
				gpuPlanTestSeriesEntry(gpuPlanTestSeriesID, gpuPlanTestSeriesName, gpuPlanTestSeriesCode, gpuPlanTestSkuCode, "1x A6000", 6, 48, 256),
				gpuPlanTestSeriesEntry(gpuCatalogTestSeriesID, gpuCatalogTestSeriesName, gpuCatalogTestSeriesCode, gpuCatalogTestSkuCode, "1x A100", 8, 80, 512),
			},
		},
	}
}

func gpuPlanTestSeriesEntry(id, name, code, skuCode, gpuDescription string, vcpu, memoryGiB, storageGB int64) map[string]any {
	specs := map[string]any{"gpuDescription": gpuDescription, "vcpu": vcpu, "memoryGiB": memoryGiB, "storageGB": storageGB}
	return map[string]any{
		"id":   id,
		"name": name,
		"code": code,
		"availability": []map[string]any{{
			"datacenterId":   gpuPlanTestDatacenterID,
			"datacenterName": "Kansas",
			"datacenterCode": "kansas",
			"gpuCounts": []map[string]any{{
				"count":         1,
				"specs":         specs,
				"availableSkus": []map[string]any{{"skuCode": skuCode, "description": "std", "specs": specs}},
			}},
		}},
	}
}

// Every read the mock serves adds the sentinel configuration, so this envelope
// omits the block.
func gpuPlanTestReadBody(name string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"id":        gpuPlanTestID,
			"name":      name,
			"createdAt": gpuPlanTestTimestamp,
			"updatedAt": gpuPlanTestTimestamp,
			"status":    "Running",
			"ip":        "10.0.0.1",
			"datacenter": map[string]any{
				"id":          gpuPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
			"sshKeyId": gpuPlanTestSshKeyID,
			"image":    gpuPlanTestAPIImageName,
		},
	}
}

// startGPUPlanMockServer serves the GPU endpoints a rename needs. The handler
// keeps the name from the last create or update. The read after an apply then
// agrees with the configuration and leaves the refresh plan empty. The first
// returned function renames the GPU out of band, which is how a test creates
// drift. The second returns the body of the last create POST.
func startGPUPlanMockServer(t *testing.T) (*httptest.Server, func(string), func() map[string]any) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	var createBody map[string]any

	gpuPath := "/v1/resource/gpu/" + gpuPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/gpu/inventory":
			testutil.WriteJSONResponse(w, gpuPlanTestInventoryBody())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/gpu/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			createBody = body
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
			testutil.WriteJSONResponse(w, gpuCatalogTestReadBody(t, current))
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

	lastCreateBody := func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return createBody
	}

	return server, setName, lastCreateBody
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
	server, _, _ := startGPUPlanMockServer(t)

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

// The test pins the in-place rename path. It does not guard a fix.
func TestGPUResourcePlanRenameWithSeriesName(t *testing.T) {
	t.Parallel()
	server, _, _ := startGPUPlanMockServer(t)

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
	server, setName, _ := startGPUPlanMockServer(t)

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

// gpuCatalogTestReadBody answers with the sentinel series the API returns when
// its own series lookup misses. The state must still carry the code the
// inventory resolved. The read echo cannot be the source of that code.
func gpuCatalogTestReadBody(t *testing.T, name string) map[string]any {
	t.Helper()

	body := gpuPlanTestReadBody(name)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("Expected a data object in the read body, got %T", body["data"])
	}
	data["configuration"] = map[string]any{
		"name":     "Unknown",
		"code":     "unknown",
		"skuCode":  gpuCatalogTestSkuCode,
		"gpuCount": 1,
		"cpu":      8,
		"ram":      80,
		"disk":     512,
	}
	return body
}

// A configuration that names a catalog series must reach the API as the series
// the inventory resolved. The create POST carries the series ID, and the state
// carries the code, so the test pins both.
func TestGPUResourcePlanAcceptsCatalogSeriesCode(t *testing.T) {
	t.Parallel()
	server, _, lastCreateBody := startGPUPlanMockServer(t)

	seriesField := fmt.Sprintf("series_name = %q", gpuCatalogTestSeriesName)

	checkCreateBody := func(*terraform.State) error {
		body := lastCreateBody()
		if body == nil {
			return fmt.Errorf("expected a create POST, got none")
		}
		if got, _ := body["seriesId"].(string); got != gpuCatalogTestSeriesID {
			return fmt.Errorf("expected create body seriesId %q, got %q", gpuCatalogTestSeriesID, got)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: gpuPlanTestConfig(server.URL, seriesField, "gpu-catalog-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_name", gpuCatalogTestSeriesName),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_code", gpuCatalogTestSeriesCode),
					checkCreateBody,
				),
			},
		},
	})
}
