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
	volPlanTestID           = "vol-1"
	volPlanTestDatacenterID = "dc-1"
	volPlanTestVolumeType   = "SSD"
	volPlanTestComponent    = "vol-add-ssd"
	volPlanTestSizeGb       = 256
	volPlanTestName         = "vol-plan-a"
	volPlanTestTimestamp    = "2026-01-02T15:04:05Z"
)

func volPlanTestSizesBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": volPlanTestDatacenterID,
			"volumeTypes": []map[string]any{{
				"componentCode": volPlanTestComponent,
				"availableSizes": []map[string]any{
					{"skuId": "sku-128", "sizeGb": 128},
					{"skuId": "sku-256", "sizeGb": volPlanTestSizeGb},
				},
			}},
		},
	}
}

func volPlanTestReadBody(name string, sizeGb int64) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"id":     volPlanTestID,
			"name":   name,
			"sizeGb": sizeGb,
			"volumeType": map[string]any{
				"id":          1,
				"name":        volPlanTestVolumeType,
				"description": "Solid state drive",
			},
			"datacenter": map[string]any{
				"id":          volPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
			"virtualMachineId": "",
			"createdAt":        volPlanTestTimestamp,
			"updatedAt":        volPlanTestTimestamp,
		},
	}
}

// startVolumePlanMockServer serves the volume endpoints a drift test needs. The handler
// keeps the name from the last create and the size from the last resize, so the read
// after an apply agrees with the configuration and leaves the refresh plan empty. The
// returned functions change the stored values out of band, which is how a test creates drift.
func startVolumePlanMockServer(t *testing.T) (*httptest.Server, func(string), func(int64)) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	sizeGb := int64(volPlanTestSizeGb)

	volumePath := "/v1/resource/volumes/" + volPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+volPlanTestDatacenterID+"/volume-sizes":
			testutil.WriteJSONResponse(w, volPlanTestSizesBody())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/volumes/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-1", "create issued")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", volPlanTestID, true)
		case r.Method == http.MethodPut && r.URL.Path == volumePath+"/resize":
			body := testutil.ReadRequestBody(r)
			newSize, _ := body["newSizeGb"].(float64)
			mu.Lock()
			sizeGb = int64(newSize)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-1", "resize issued")
		case r.Method == http.MethodGet && r.URL.Path == volumePath:
			mu.Lock()
			currentName, currentSize := name, sizeGb
			mu.Unlock()
			testutil.WriteJSONResponse(w, volPlanTestReadBody(currentName, currentSize))
		case r.Method == http.MethodDelete && r.URL.Path == volumePath:
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

	setSize := func(newSize int64) {
		mu.Lock()
		sizeGb = newSize
		mu.Unlock()
	}

	return server, setName, setSize
}

func volPlanTestConfig(host string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_volume" "test" {
  name          = %q
  datacenter_id = %q
  volume_type   = %q
  size_gb       = %d
}
`, host, volPlanTestName, volPlanTestDatacenterID, volPlanTestVolumeType, volPlanTestSizeGb)
}

func TestVolumeResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	server, setName, _ := startVolumePlanMockServer(t)

	config := volPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volPlanTestName),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "id", volPlanTestID),
				),
			},
			{
				PreConfig: func() { setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// The name carries RequiresReplace, so the drift the refresh finds plans a replacement.
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volPlanTestName),
				),
			},
		},
	})
}

func TestVolumeResourcePlanDetectsOutOfBandResize(t *testing.T) {
	t.Parallel()
	server, _, setSize := startVolumePlanMockServer(t)

	config := volPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
				),
			},
			{
				PreConfig: func() { setSize(128) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
				),
			},
		},
	})
}
