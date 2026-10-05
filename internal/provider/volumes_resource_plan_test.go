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
	volPlanTestDefaultGroup = "rg-default"
	volPlanTestGroupA       = "rg-a"
	volPlanTestGroupB       = "rg-b"
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

func volPlanTestReadBody(name string, sizeGb int64, groupId string) map[string]any {
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
			"resourceGroupId":  groupId,
			"virtualMachineId": "",
			"createdAt":        volPlanTestTimestamp,
			"updatedAt":        volPlanTestTimestamp,
		},
	}
}

// volPlanTestRecorder holds what the volume mock saw and the group it reports.
type volPlanTestRecorder struct {
	mu          sync.Mutex
	groupId     string
	createBody  map[string]any
	moveBodies  []map[string]any
	resizeCount int
	writeOrder  []string

	refuseMoveOnce  bool
	defaultOnResize string
}

func (rec *volPlanTestRecorder) setGroup(groupId string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.groupId = groupId
}

func (rec *volPlanTestRecorder) snapshot() (createBody map[string]any, moveBodies []map[string]any, resizeCount int, writeOrder []string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.createBody, slices.Clone(rec.moveBodies), rec.resizeCount, slices.Clone(rec.writeOrder)
}

// startVolumePlanMockServer serves the volume endpoints a drift test needs.
// The handler keeps the name and size that the last create or resize set.
// The read after an apply then agrees with the configuration.
// The returned functions change the stored values out of band to create drift.
func startVolumePlanMockServer(t *testing.T) (*httptest.Server, func(string), func(int64)) {
	t.Helper()
	server, setName, setSize, _ := startVolumePlanRecordingMockServer(t)
	return server, setName, setSize
}

// startVolumePlanRecordingMockServer also stores the resource group that a
// create or a move sent, reports the Default group otherwise, and records
// every write so a test can assert the wire.
func startVolumePlanRecordingMockServer(t *testing.T) (*httptest.Server, func(string), func(int64), *volPlanTestRecorder) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	sizeGb := int64(volPlanTestSizeGb)
	rec := &volPlanTestRecorder{groupId: volPlanTestDefaultGroup}

	volumePath := "/v1/resource/volumes/" + volPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+volPlanTestDatacenterID+"/volume-sizes":
			testutil.WriteJSONResponse(w, volPlanTestSizesBody())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/volumes/":
			body := testutil.ReadRequestBody(r)
			skuId, _ := body["skuId"].(string)
			mu.Lock()
			name, _ = body["name"].(string)
			sizeGb = volPlanTestSkuSizes[skuId]
			mu.Unlock()
			rec.mu.Lock()
			rec.createBody = body
			if groupId, ok := body["resourceGroupId"].(string); ok {
				rec.groupId = groupId
			}
			rec.mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-1", "create issued")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", volPlanTestID, true)
		case r.Method == http.MethodPut && r.URL.Path == volumePath+"/resize":
			body := testutil.ReadRequestBody(r)
			newSize, _ := body["newSizeGb"].(float64)
			mu.Lock()
			sizeGb = int64(newSize)
			mu.Unlock()
			rec.mu.Lock()
			rec.resizeCount++
			rec.writeOrder = append(rec.writeOrder, "resize")
			if rec.groupId == "" && rec.defaultOnResize != "" {
				rec.groupId = rec.defaultOnResize
			}
			rec.mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-1", "resize issued")
		case r.Method == http.MethodPut && r.URL.Path == volumePath:
			body := testutil.ReadRequestBody(r)
			rec.mu.Lock()
			if rec.refuseMoveOnce {
				rec.refuseMoveOnce = false
				rec.mu.Unlock()
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"message":"You do not have access to the specified resource group","error":{"code":"Entity Access Denied"}}`))
				return
			}
			rec.moveBodies = append(rec.moveBodies, body)
			rec.writeOrder = append(rec.writeOrder, "move")
			if groupId, ok := body["resourceGroupId"].(string); ok {
				rec.groupId = groupId
			}
			rec.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "moved"})
		case r.Method == http.MethodGet && r.URL.Path == volumePath:
			mu.Lock()
			currentName, currentSize := name, sizeGb
			mu.Unlock()
			rec.mu.Lock()
			currentGroup := rec.groupId
			rec.mu.Unlock()
			testutil.WriteJSONResponse(w, volPlanTestReadBody(currentName, currentSize, currentGroup))
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

	return server, setName, setSize, rec
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
	return volPlanTestConfigWithGroup(host, volumeType, sizeGb, "")
}

// An empty groupId leaves resource_group_id out of the configuration.
func volPlanTestConfigWithGroup(host, volumeType string, sizeGb int64, groupId string) string {
	groupLine := ""
	if groupId != "" {
		groupLine = fmt.Sprintf("resource_group_id = %q", groupId)
	}
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
  %s
}
`, host, volPlanTestName, volPlanTestDatacenterID, volumeType, sizeGb, groupLine)
}

// The schema rules only on the spelling of a built-in class. The datacenter
// catalog gates every other code, and its refusal names the codes it offers.
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
			"resourceGroupId":  volPlanTestDefaultGroup,
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
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
				Config:          config,
				ResourceName:    gpcnVolumeTest,
				ImportStateKind: resource.ImportBlockWithID,
				ImportState:     true,
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

// The unit table pins the refusal bytes. This case proves the validator hangs
// on the schema and stops a plan.
func TestVolumeResourcePlanRefusesDivergentSpelling(t *testing.T) {
	t.Parallel()
	server, _, _ := startVolumePlanMockServer(t)

	cases := map[string]string{
		"ssd":          "SSD",
		"vol-add-ssd":  "SSD",
		"Nvme":         "NVMe",
		"vol-add-nvme": "NVMe",
	}
	for volumeType, alias := range cases {
		t.Run(volumeType, func(t *testing.T) {
			// Terraform wraps a diagnostic, so the suggestion can start a line.
			expected := regexp.MustCompile(`use\s+` + regexp.QuoteMeta(`"`+alias+`"`))
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      volPlanTestConfigWithType(server.URL, volumeType),
						ExpectError: expected,
					},
				},
			})
		})
	}
}

const volPlanTestThirdClassName = "Ultra"

// startThirdClassVolumePlanMockServer answers for a storage class outside the
// two built-in ones. The volume reports a resolved code, and the catalog
// offers that code, so a configuration can name it.
func startThirdClassVolumePlanMockServer(t *testing.T) *httptest.Server {
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
				"code":        volPlanTestUnknownComponent,
				"name":        volPlanTestThirdClassName,
				"description": "Ultra fast drive",
			},
			"datacenter": map[string]any{
				"id":          volPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
			"resourceGroupId":  volPlanTestDefaultGroup,
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
				"name":          volPlanTestThirdClassName,
				"availableSizes": []map[string]any{
					{"skuId": "sku-ultra-256", "sizeGb": volPlanTestSizeGb, "displayName": "Ultra 256 GB"},
				},
			}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
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

// The alias rewrite serves the two built-in classes. A third class must keep
// the code the API reports, because the configuration names the code itself.
// Only terraform shows that the round trip plans nothing.
func TestVolumeResourcePlanImportKeepsAThirdClassCode(t *testing.T) {
	t.Parallel()
	server := startThirdClassVolumePlanMockServer(t)

	config := volPlanTestConfigWithType(server.URL, volPlanTestUnknownComponent)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", volPlanTestUnknownComponent),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type_code", volPlanTestUnknownComponent),
				),
			},
			{
				Config:          config,
				ResourceName:    gpcnVolumeTest,
				ImportStateKind: resource.ImportBlockWithID,
				ImportState:     true,
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

func volPlanTestExpectNoWrites(t *testing.T, rec *volPlanTestRecorder) {
	t.Helper()
	_, moveBodies, resizeCount, _ := rec.snapshot()
	if len(moveBodies) != 0 || resizeCount != 0 {
		t.Errorf("expected no write request, saw %d move(s) and %d resize(s)", len(moveBodies), resizeCount)
	}
}

func TestVolumeResourcePlanCreateWithoutGroupAdoptsDefault(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfig(server.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue(gpcnVolumeTest, tfjsonpath.New("resource_group_id")),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestDefaultGroup),
			},
		},
	})

	createBody, _, _, _ := rec.snapshot()
	if _, present := createBody["resourceGroupId"]; present {
		t.Errorf("create body must not carry resourceGroupId, got %v", createBody["resourceGroupId"])
	}
}

func TestVolumeResourcePlanCreateWithGroupSendsIt(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA),
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupA),
			},
		},
	})

	createBody, _, _, _ := rec.snapshot()
	if createBody["resourceGroupId"] != volPlanTestGroupA {
		t.Errorf("expected create body resourceGroupId %q, got %v", volPlanTestGroupA, createBody["resourceGroupId"])
	}
}

func TestVolumeResourcePlanMovesGroupInPlace(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA),
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupA),
			},
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupB),
			},
		},
	})

	_, moveBodies, resizeCount, _ := rec.snapshot()
	if len(moveBodies) != 1 {
		t.Fatalf("expected one move request, saw %d", len(moveBodies))
	}
	if keys := slices.Sorted(maps.Keys(moveBodies[0])); !slices.Equal(keys, []string{"resourceGroupId"}) {
		t.Errorf("expected the move body key set {resourceGroupId}, got %v", keys)
	}
	if moveBodies[0]["resourceGroupId"] != volPlanTestGroupB {
		t.Errorf("expected move to %q, got %v", volPlanTestGroupB, moveBodies[0]["resourceGroupId"])
	}
	if resizeCount != 0 {
		t.Errorf("expected no /resize call, saw %d", resizeCount)
	}
}

func TestVolumeResourcePlanRemovedGroupPlansEmpty(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA),
			},
			{
				Config: volPlanTestConfig(server.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupA),
			},
		},
	})

	volPlanTestExpectNoWrites(t, rec)
}

func TestVolumeResourcePlanDetectsOutOfBandMove(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	config := volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() { rec.setGroup(volPlanTestGroupB) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupA),
			},
		},
	})

	_, moveBodies, _, _ := rec.snapshot()
	if len(moveBodies) != 1 || moveBodies[0]["resourceGroupId"] != volPlanTestGroupA {
		t.Errorf("expected one move back to %q, saw %v", volPlanTestGroupA, moveBodies)
	}
}

func TestVolumeResourcePlanImportReadsGroup(t *testing.T) {
	t.Parallel()
	server, _, _, _ := startVolumePlanRecordingMockServer(t)

	config := volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
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

func TestVolumeResourcePlanGrowDoesNotMove(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, 128, volPlanTestGroupA),
			},
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
			},
		},
	})

	_, moveBodies, resizeCount, _ := rec.snapshot()
	if resizeCount != 1 || len(moveBodies) != 0 {
		t.Errorf("expected one resize and no move, saw %d resize(s) and %d move(s)", resizeCount, len(moveBodies))
	}
}

func TestVolumeResourcePlanMoveAndGrow(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, 128, volPlanTestGroupA),
			},
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", fmt.Sprint(volPlanTestSizeGb)),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupB),
				),
			},
		},
	})

	_, moveBodies, _, writeOrder := rec.snapshot()
	if !slices.Equal(writeOrder, []string{"move", "resize"}) {
		t.Errorf("expected the move before the resize, saw %v", writeOrder)
	}
	if len(moveBodies) != 1 || len(moveBodies[0]) != 1 {
		t.Errorf("expected one bare move body, saw %v", moveBodies)
	}
}

func TestVolumeResourcePlanGrowWithUnsetGroupKeepsItKnown(t *testing.T) {
	t.Parallel()
	server, _, _, _ := startVolumePlanRecordingMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithSize(server.URL, 128),
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestDefaultGroup),
			},
			{
				Config: volPlanTestConfigWithSize(server.URL, volPlanTestSizeGb),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(gpcnVolumeTest, tfjsonpath.New("resource_group_id"), knownvalue.StringExact(volPlanTestDefaultGroup)),
					},
				},
			},
		},
	})
}

// The mock reports no group until the resize, which leaves state null as a 1.4.0
// state with no group is.
func TestVolumeResourcePlanNullGroupStateSurvivesAnUpdate(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)
	rec.mu.Lock()
	rec.groupId = ""
	rec.defaultOnResize = volPlanTestDefaultGroup
	rec.mu.Unlock()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithSize(server.URL, 128),
				Check:  resource.TestCheckNoResourceAttr(gpcnVolumeTest, "resource_group_id"),
			},
			{
				Config: volPlanTestConfigWithSize(server.URL, volPlanTestSizeGb),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestDefaultGroup),
			},
		},
	})
}

func TestVolumeResourcePlanRefusedMoveShowsAPIError(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)
	moveConfig := volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupB)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: volPlanTestConfigWithGroup(server.URL, volPlanTestVolumeType, volPlanTestSizeGb, volPlanTestGroupA),
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupA),
			},
			{
				PreConfig: func() {
					rec.mu.Lock()
					rec.refuseMoveOnce = true
					rec.mu.Unlock()
				},
				Config:      moveConfig,
				ExpectError: regexp.MustCompile(`You\s+do\s+not\s+have\s+access\s+to\s+the\s+specified\s+resource\s+group`),
			},
			{
				Config: moveConfig,
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupB),
			},
		},
	})
}

func TestVolumeResourcePlanAdoptsOutOfBandMoveWhenUnset(t *testing.T) {
	t.Parallel()
	server, _, _, rec := startVolumePlanRecordingMockServer(t)
	config := volPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestDefaultGroup),
			},
			{
				PreConfig: func() { rec.setGroup(volPlanTestGroupB) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVolumeTest, "resource_group_id", volPlanTestGroupB),
			},
		},
	})

	volPlanTestExpectNoWrites(t, rec)
}
