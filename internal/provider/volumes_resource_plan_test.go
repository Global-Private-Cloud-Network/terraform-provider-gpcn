package provider

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
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

var volPlanTestSkuSizes = map[string]int64{"sku-128": 128, "sku-256": volPlanTestSizeGb}

func volPlanTestSizesBody() map[string]any {
	availableSizes := make([]map[string]any, 0, len(volPlanTestSkuSizes))
	for _, skuId := range slices.Sorted(maps.Keys(volPlanTestSkuSizes)) {
		availableSizes = append(availableSizes, map[string]any{"skuId": skuId, "sizeGb": volPlanTestSkuSizes[skuId]})
	}
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": volPlanTestDatacenterID,
			"volumeTypes": []map[string]any{{
				"componentCode":  volPlanTestComponent,
				"availableSizes": availableSizes,
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
				"code":        volPlanTestComponent,
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

// startVolumePlanMockServer serves the volume endpoints a drift test needs.
// The handler keeps the name and size that the last create or resize set.
// The read after an apply then agrees with the configuration.
// The returned functions change the stored values out of band to create drift.
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
			skuId, _ := body["skuId"].(string)
			mu.Lock()
			name, _ = body["name"].(string)
			sizeGb = volPlanTestSkuSizes[skuId]
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
	return volPlanTestConfigWithSize(host, volPlanTestSizeGb)
}

func volPlanTestConfigWithSize(host string, sizeGb int64) string {
	return volPlanTestConfigFor(host, volPlanTestVolumeType, sizeGb)
}

func volPlanTestConfigWithType(host, volumeType string) string {
	return volPlanTestConfigFor(host, volumeType, volPlanTestSizeGb)
}

func volPlanTestConfigFor(host, volumeType string, sizeGb int64) string {
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
`, host, volPlanTestName, volPlanTestDatacenterID, volumeType, sizeGb)
}

// The schema no longer rules on the spelling of a volume type. The datacenter
// catalog is the gate, and its refusal names the codes the datacenter offers.
func TestVolumeResourcePlanRefusesUnknownTypeAtLookup(t *testing.T) {
	t.Parallel()
	server, _, _ := startVolumePlanMockServer(t)

	for _, volumeType := range []string{"vol-add-ultra", "vm-root-disk-ssd"} {
		t.Run(volumeType, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      volPlanTestConfigWithType(server.URL, volumeType),
						ExpectError: regexp.MustCompile("not available for this datacenter"),
					},
				},
			})
		})
	}
}

// Read keeps the configured name and size_gb instead of refreshing them.
// Reconciling a drifted name replaces the volume, and reconciling a drifted grow replaces it too.
func TestVolumeResourcePlanIgnoresOutOfBandRename(t *testing.T) {
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
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volPlanTestName),
				),
			},
		},
	})
}

func TestVolumeResourcePlanIgnoresOutOfBandShrink(t *testing.T) {
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
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
				),
			},
		},
	})
}

func TestVolumeResourcePlanIgnoresOutOfBandGrow(t *testing.T) {
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
				PreConfig: func() { setSize(512) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
				),
			},
		},
	})
}

// A plan that changes anything marks every computed attribute with a null
// configuration value as unknown. The storage class cannot change in place, so a
// resize must not offer the code as known after apply.
func TestVolumeResourcePlanKeepsTypeCodeOnResize(t *testing.T) {
	t.Parallel()
	server, _, _ := startVolumePlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithSize(server.URL, 128),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type_code", volPlanTestComponent),
				),
			},
			{
				Config: volPlanTestConfigWithSize(server.URL, volPlanTestSizeGb),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(gpcnVolumeTest, tfjsonpath.New("volume_type_code"), knownvalue.StringExact(volPlanTestComponent)),
					},
				},
			},
		},
	})
}

const (
	volPlanTestUnknownComponent = "vol-add-ultra"
	volPlanTestUnknownName      = "Unknown"
)

// startUnknownVolumeTypePlanMockServer answers for a volume whose SKU the
// platform cannot resolve.
func startUnknownVolumeTypePlanMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	volumePath := "/v1/resource/volumes/" + volPlanTestID

	readBody := map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"id":     volPlanTestID,
			"name":   volPlanTestName,
			"sizeGb": volPlanTestSizeGb,
			"volumeType": map[string]any{
				"code":        nil,
				"name":        volPlanTestUnknownName,
				"description": "",
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

	sizesBody := map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": volPlanTestDatacenterID,
			"volumeTypes": []map[string]any{{
				"componentCode": volPlanTestUnknownComponent,
				"name":          volPlanTestUnknownName,
				"description":   nil,
				"availableSizes": []map[string]any{
					{"skuId": "sku-ultra-256", "sizeGb": volPlanTestSizeGb, "displayName": "Ultra 256 GB"},
				},
			}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+volPlanTestDatacenterID+"/volume-sizes":
			testutil.WriteJSONResponse(w, sizesBody)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/volumes/":
			testutil.HandleCreateJobResponse(w, "job-1", "create issued")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", volPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == volumePath:
			testutil.WriteJSONResponse(w, readBody)
		case r.Method == http.MethodDelete && r.URL.Path == volumePath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func TestVolumeResourcePlanImportsUnknownTypeName(t *testing.T) {
	t.Parallel()
	server := startUnknownVolumeTypePlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithType(server.URL, volPlanTestUnknownComponent),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", volPlanTestUnknownComponent),
					resource.TestCheckNoResourceAttr(gpcnVolumeTest, "volume_type_code"),
					resource.TestCheckNoResourceAttr(gpcnVolumeTest, "volume_type_id"),
				),
			},
			{
				ResourceName:    gpcnVolumeTest,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          volPlanTestConfigWithType(server.URL, volPlanTestUnknownName),
			},
		},
	})
}

// Only terraform can show that the alias round trip plans nothing.
func TestVolumeResourcePlanImportAliasPlansEmpty(t *testing.T) {
	t.Parallel()
	server, _, _ := startVolumePlanMockServer(t)

	config := volPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", volPlanTestVolumeType),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type_code", volPlanTestComponent),
				),
			},
			{
				Config:            config,
				ResourceName:      gpcnVolumeTest,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}
