package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/networks"
	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/virtualmachines"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	vmPlanTestID           = "vm-1"
	vmPlanTestDatacenterID = "dc-1"
	vmPlanTestSizeID       = "sku-1"
	vmPlanTestSizeID2      = "sku-2"
	vmPlanTestSizeCode     = "G-Small-1"
	vmPlanTestImageID      = "img-1"
	vmPlanTestImageName    = "ubuntu-22.04"
	vmPlanTestSubnetID     = "subnet-1"
	vmPlanTestVpcID        = "vpc-1"
	vmPlanTestSegmentID    = "segment-1"
	vmPlanTestSegmentID2   = "segment-2"
	vmPlanTestSegmentID3   = "segment-3"
	vmPlanTestSshKeyID     = "key-1"
	vmPlanTestUsername     = "ubuntu"
	vmPlanTestTimestamp    = "2026-01-02T15:04:05Z"
	vmPlanTestPath         = "/v1/resource/virtual-machines/" + vmPlanTestID
)

// shortenVirtualMachinePolling removes the waits the create, stop, and delete paths
// take. The package variables carry production defaults that make this test minutes long.
// A caller must stay sequential, because Go resumes a parallel test only after every
// sequential test ends.
func shortenVirtualMachinePolling(t *testing.T) {
	t.Helper()

	previousInterval := virtualmachines.VM_STATUS_POLL_INTERVAL
	previousSettle := virtualmachines.VM_STATUS_SETTLE_WAIT
	previousDelay := virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS
	virtualmachines.VM_STATUS_POLL_INTERVAL = 5 * time.Millisecond
	virtualmachines.VM_STATUS_SETTLE_WAIT = 5 * time.Millisecond
	virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS = 0
	t.Cleanup(func() {
		virtualmachines.VM_STATUS_POLL_INTERVAL = previousInterval
		virtualmachines.VM_STATUS_SETTLE_WAIT = previousSettle
		virtualmachines.DEFAULT_INITIAL_POLL_DELAY_SECONDS = previousDelay
	})
}

func vmPlanTestSizesBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": vmPlanTestDatacenterID,
			"categories":   []map[string]any{},
		},
	}
}

// vmPlanTestUpgradeSizesBody lists every size but the one the machine carries. GPCN
// keeps the current SKU out of the upgrade list, so a size the list names is an in-place
// resize.
func vmPlanTestUpgradeSizesBody(currentSkuId string) map[string]any {
	sizes := []map[string]any{}
	for index, skuId := range []string{vmPlanTestSizeID, vmPlanTestSizeID2} {
		if skuId == currentSkuId {
			continue
		}
		sizes = append(sizes, map[string]any{
			"skuId":   skuId,
			"name":    fmt.Sprintf("G-Small-%d", index+1),
			"skuCode": fmt.Sprintf("g-small-%d", index+1),
			"cpu":     2 * (index + 1),
			"ram":     4 * (index + 1),
			"disk":    80,
		})
	}

	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"datacenterId": vmPlanTestDatacenterID,
			"categories": []map[string]any{{
				"code":  "general-purpose",
				"name":  "General Purpose",
				"sizes": sizes,
			}},
		},
	}
}

// An import has no image_id, so the mapper resolves it from the image name the detail
// projection reports.
func vmPlanTestImagesBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":        1,
			"name":      "Linux",
			"sortOrder": 1,
			"images": []map[string]any{{
				"id":   vmPlanTestImageID,
				"name": vmPlanTestImageName,
			}},
		}},
	}
}

func vmPlanTestReadBody(name, status, skuId string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"status":    status,
			"id":        vmPlanTestID,
			"name":      name,
			"createdAt": vmPlanTestTimestamp,
			"updatedAt": vmPlanTestTimestamp,
			"configuration": map[string]any{
				"name":    vmPlanTestSizeCode,
				"skuId":   skuId,
				"skuCode": "g-small-1",
				"cpu":     2,
				"ram":     4,
				"disk":    80,
			},
			"image":          vmPlanTestImageName,
			"username":       vmPlanTestUsername,
			"sshKeyId":       vmPlanTestSshKeyID,
			"networkHotplug": 1,
			"datacenter": map[string]any{
				"id":          vmPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
		},
	}
}

// The birth subnet comes from the create body. The interface the API reports is
// therefore the one the provider asked for. A fixed fixture could agree by coincidence.
// Every legacy column is null, which is what a VPC interface carries.
func vmPlanTestNetworkInterfacesBody(birthSubnetID string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":               "nic-1",
			"networkInterface": 1,
			"isPrimary":        1,
			"macAddress":       "fa:16:3e:00:00:01",
			"publicIp":         nil,
			"publicIpId":       nil,
			"privateIp":        "10.0.0.5",
			"world":            "vpc",
			"networkName":      nil,
			"networkId":        nil,
			"cidrBlock":        "10.0.0.0/24",
			"gatewayIp":        nil,
			"networkType":      nil,
			"vpcSubnetId":      birthSubnetID,
			"subnetName":       "web",
			"vpcId":            vmPlanTestVpcID,
			"vpcName":          "prod",
			"l2SegmentId":      nil,
			"l2SegmentName":    nil,
		}},
	}
}

// startVirtualMachinePlanMockServer serves the endpoints a drift test needs. The handler
// keeps the name from the last create or update. The read after an apply then agrees with
// the configuration and leaves the refresh plan empty. The returned functions change the
// stored name and SKU out of band, which is how a test creates drift.
// The sizes arm reports no upgrade target, so a planned size change asks for a replacement.
func startVirtualMachinePlanMockServer(t *testing.T) (*httptest.Server, func(string), func(string)) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	skuId := vmPlanTestSizeID
	status := virtualmachines.VMStatusRunning.String()

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentSubnet))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-images":
			testutil.WriteJSONResponse(w, vmPlanTestImagesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPath:
			mu.Lock()
			currentName, currentStatus, currentSku := name, status, skuId
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, currentSku))
		case r.Method == http.MethodPut && r.URL.Path == vmPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPath:
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

	setSkuId := func(newSkuId string) {
		mu.Lock()
		skuId = newSkuId
		mu.Unlock()
	}

	return server, setName, setSkuId
}

func vmPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  allocate_public_ip = false
  subnet_id          = %q
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)
}

func TestVirtualMachineResourcePlanDetectsOutOfBandRename(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setName, _ := startVirtualMachinePlanMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-a"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig: func() { setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-a"),
				),
			},
		},
	})
}

// A refreshed size_id would plan a downgrade that the API refuses, and Terraform would
// then replace the VM. Read keeps the configured value to prevent that.
func TestVirtualMachineResourcePlanIgnoresOutOfBandResize(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, setSkuId := startVirtualMachinePlanMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-b")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID),
				),
			},
			{
				PreConfig: func() { setSkuId("sku-2") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID),
				),
			},
		},
	})
}

// startVirtualMachineSizeMockServer keeps the SKU that the machine detail reports and
// records the size verbs in the order they arrive. A test moves the live SKU to stand
// for a resize that the platform already took. The sizes arm keeps the SKU the machine
// carries out of its own upgrade list, which is what GPCN does.
func startVirtualMachineSizeMockServer(t *testing.T, hotplug int) (*httptest.Server, func(string), func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	skuId := vmPlanTestSizeID
	var events []string
	status := virtualmachines.VMStatusRunning.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath+"/size":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			events = append(events, "resize")
			if requested, ok := body["skuId"].(string); ok {
				skuId = requested
			}
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-5", "resize issued")
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			events = append(events, "stop")
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			mu.Lock()
			events = append(events, "start")
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentSubnet))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			mu.Lock()
			currentSku := skuId
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestUpgradeSizesBody(currentSku))
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus, currentSku := name, status, skuId
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, currentSku, hotplug))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	setSkuId := func(newSkuId string) {
		mu.Lock()
		skuId = newSkuId
		mu.Unlock()
	}

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, setSkuId, recorded
}

// vmSizePlanTestConfig names the SKU the configuration asks for.
func vmSizePlanTestConfig(host, name, sizeID string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host        = %q
  api_key     = "test-key"
  max_retries = 0
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  allocate_public_ip = false
  subnet_id          = %q
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, sizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)
}

// A resize that the platform took before a failed read-back leaves state behind the
// machine. GPCN keeps the current SKU out of its own upgrade list, so the next plan
// proposed a replacement. The live SKU now answers the question, and the apply that
// follows sends no resize.
func TestVirtualMachineResourcePlanTreatsAnAppliedResizeAsDone(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setSkuId, recorded := startVirtualMachineSizeMockServer(t, 1)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSizePlanTestConfig(server.URL, "vm-plan-applied-resize", vmPlanTestSizeID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID),
				),
			},
			{
				PreConfig: func() { setSkuId(vmPlanTestSizeID2) },
				Config:    vmSizePlanTestConfig(server.URL, "vm-plan-applied-resize", vmPlanTestSizeID2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "size_id", vmPlanTestSizeID2),
					func(*terraform.State) error {
						if events := recorded(); slices.Contains(events, "resize") {
							return fmt.Errorf("expected no resize call, got %v", events)
						}
						return nil
					},
				),
			},
		},
	})
}

// A resize the platform took before a failed read-back leaves state behind the machine.
// The next apply asks for the SKU the machine already carries. The stop decision reads
// the live SKU, so that retry costs the user no downtime.
func TestVirtualMachineResourcePlanSizeRetryStopsNothing(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setSkuId, recorded := startVirtualMachineSizeMockServer(t, 0)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSizePlanTestConfig(server.URL, "vm-plan-size-retry", vmPlanTestSizeID),
			},
			{
				PreConfig: func() { setSkuId(vmPlanTestSizeID2) },
				Config:    vmSizePlanTestConfig(server.URL, "vm-plan-size-retry", vmPlanTestSizeID2),
				Check: func(*terraform.State) error {
					if events := recorded(); len(events) != 0 {
						return fmt.Errorf("expected the retry to call nothing, got %v", events)
					}
					return nil
				},
			},
		},
	})
}

// An image without network hotplug takes a resize only on a stopped machine. The live
// SKU still differs from the plan here. The provider stops the machine, resizes it, and
// starts it again.
func TestVirtualMachineResourcePlanResizeStopsTheMachine(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, recorded := startVirtualMachineSizeMockServer(t, 0)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSizePlanTestConfig(server.URL, "vm-plan-size-stop", vmPlanTestSizeID),
			},
			{
				Config: vmSizePlanTestConfig(server.URL, "vm-plan-size-stop", vmPlanTestSizeID2),
				Check: func(*terraform.State) error {
					want := []string{"stop", "resize", "start"}
					if events := recorded(); !slices.Equal(events, want) {
						return fmt.Errorf("expected %v, got %v", want, events)
					}
					return nil
				},
			},
		},
	})
}

// startVirtualMachineDestroyMockServer refuses to delete. A test that reaches the delete
// endpoint has not treated the machine as gone.
func startVirtualMachineDestroyMockServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentSubnet))
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/stop":
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodDelete && r.URL.Path == vmPath:
			t.Error("Expected no delete call for a machine the platform already removed")
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	setDeleting := func() {
		mu.Lock()
		status = virtualmachines.VMStatusDeleting.String()
		mu.Unlock()
	}

	return server, setDeleting
}

// A machine the platform is already removing never reaches a stopped status. The
// pre-delete stop fails fast on a status the machine never leaves. The outcome matches a
// 404: the machine is gone, so it leaves state without an error.
func TestVirtualMachineResourcePlanDestroyTreatsDeletingAsGone(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, setDeleting := startVirtualMachineDestroyMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-destroy")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig: setDeleting,
				Config:    config,
				Destroy:   true,
			},
		},
	})
}

// GPCN refuses a held address beside a request for a new one. The provider says so at
// validate time, so the operator never pays for the round trip that would say it.
func TestVirtualMachineResourcePlanRefusesPublicIpIdWithAllocatePublicIp(t *testing.T) {
	config := fmt.Sprintf(`
provider "gpcn" {
  host    = "http://127.0.0.1:1"
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name               = "vm-plan-conflict"
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  subnet_id          = %q
  allocate_public_ip = true
  public_ip_id       = "address-1"
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`public_ip_id and allocate_public_ip are mutually exclusive`),
			},
		},
	})
}

// An import reads the machine and its interfaces, and nothing else. subnet_id,
// l2_segment_ids, allocate_public_ip and public_ip live only on the interface list. An
// import that misses it hands back a state a plan cannot reconcile.
func TestVirtualMachineResourcePlanImportsVpcIdentity(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, _ := startVirtualMachinePlanMockServer(t)

	config := vmPlanTestConfig(server.URL, "vm-plan-import")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "subnet_id", vmPlanTestSubnetID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "0"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "allocate_public_ip", "false"),
					resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "public_ip"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.world", "vpc"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.vpc_subnet_id", vmPlanTestSubnetID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.vpc_id", vmPlanTestVpcID),
					resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.network_id"),
				),
			},
			{
				ResourceName:      gpcnVirtualMachineTest,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A state file written by 1.3.0 carries network_ids, and the attribute is gone. Terraform
// hands that state to UpgradeResourceState before anything else reads it. The framework
// unmarshals it with IgnoreUndefinedAttributes
// (fwserver/server_upgraderesourcestate.go:60-65), so the retired attribute is dropped
// rather than refused. A refusal here would strand every machine already in state.
func TestVirtualMachineResourcePlanLoadsPriorStateWithNetworkIds(t *testing.T) {
	priorState := `{
		"id": "` + vmPlanTestID + `",
		"name": "vm-prior",
		"datacenter_id": "` + vmPlanTestDatacenterID + `",
		"size_id": "` + vmPlanTestSizeID + `",
		"image_id": "` + vmPlanTestImageID + `",
		"allocate_public_ip": false,
		"public_ip": "",
		"network_ids": ["net-1", "net-2"],
		"network_hotplug": true,
		"network_interfaces": [
			{"id": "nic-1", "network_interface": 0, "is_primary": true, "public_ip": "",
			 "public_ip_id": "", "private_ip": "10.0.0.5", "network_name": "net-standard",
			 "network_id": "net-1", "cidr_block": "10.0.0.0/24", "gateway_ip": "10.0.0.1",
			 "network_type": "standard"}
		],
		"resource_group_id": null,
		"initial_auth": {"ssh_key_id": "` + vmPlanTestSshKeyID + `", "username": "` + vmPlanTestUsername + `", "password": null}
	}`

	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatalf("building the provider server failed: %v", err)
	}

	resp, err := server.UpgradeResourceState(context.Background(), &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "gpcn_virtualmachine",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: []byte(priorState)},
	})
	if err != nil {
		t.Fatalf("UpgradeResourceState returned an error: %v", err)
	}
	for _, diagnostic := range resp.Diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("Expected no error diagnostic, got '%s': %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
	if resp.UpgradedState == nil {
		t.Fatal("Expected the upgraded state to be returned")
	}
}

// GPCN caps a machine at five interfaces, and the birth subnet holds one of them.
func TestVirtualMachineResourcePlanRefusesMoreThanFourSegments(t *testing.T) {
	config := fmt.Sprintf(`
provider "gpcn" {
  host    = "http://127.0.0.1:1"
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name           = "vm-plan-segments"
  datacenter_id  = %q
  size_id        = %q
  image_id       = %q
  subnet_id      = %q
  l2_segment_ids = ["a", "b", "c", "d", "e"]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSshKeyID, vmPlanTestUsername)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`list must contain at most 4 elements`),
			},
		},
	})
}

// GPCN refuses a second interface on one segment, so a repeated id can only fail at
// apply. The schema therefore refuses it at plan time.
func TestVirtualMachineResourcePlanRefusesADuplicateSegment(t *testing.T) {
	config := fmt.Sprintf(`
provider "gpcn" {
  host    = "http://127.0.0.1:1"
  api_key = "test-key"
}

resource "gpcn_virtualmachine" "test" {
  name           = "vm-plan-duplicate-segment"
  datacenter_id  = %q
  size_id        = %q
  image_id       = %q
  subnet_id      = %q
  l2_segment_ids = [%q, %q]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSegmentID, vmPlanTestSegmentID, vmPlanTestSshKeyID, vmPlanTestUsername)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)Duplicate\s+List\s+Value.*This\s+attribute\s+contains\s+duplicate\s+values\s+of:\s+"` + vmPlanTestSegmentID + `"`),
			},
		},
	})
}

func vmPlanTestReadBodyWithHotplug(name, status, skuId string, hotplug int) map[string]any {
	body := vmPlanTestReadBody(name, status, skuId)
	body["data"].(map[string]any)["networkHotplug"] = hotplug
	return body
}

// vmSegmentNic pairs an interface id with the segment it carries. A mock then keeps the
// id stable while the list around it changes.
type vmSegmentNic struct {
	nicID     string
	segmentID string
}

// vmSegmentNicsFromIds numbers the interfaces the way GPCN does, from the slot after the
// birth interface.
func vmSegmentNicsFromIds(segmentIDs []string) []vmSegmentNic {
	nics := make([]vmSegmentNic, 0, len(segmentIDs))
	for index, segmentID := range segmentIDs {
		nics = append(nics, vmSegmentNic{nicID: fmt.Sprintf("nic-%d", index+2), segmentID: segmentID})
	}
	return nics
}

// vmSegmentPlanTestNetworkInterfacesBody lists the birth subnet interface and one L2 row
// per segment that attached. A test can then observe the state a failed attach leaves
// behind. An L2 interface carries no address and no subnet, so those columns are null.
func vmSegmentPlanTestNetworkInterfacesBody(birthSubnetID string, attached []vmSegmentNic) map[string]any {
	rows := make([]map[string]any, 0, 1+len(attached))
	rows = append(rows, map[string]any{
		"id":               "nic-1",
		"networkInterface": 1,
		"isPrimary":        1,
		"macAddress":       "fa:16:3e:00:00:01",
		"publicIp":         nil,
		"publicIpId":       nil,
		"privateIp":        "10.0.0.5",
		"world":            "vpc",
		"networkName":      nil,
		"networkId":        nil,
		"cidrBlock":        "10.0.0.0/24",
		"gatewayIp":        nil,
		"networkType":      nil,
		"vpcSubnetId":      birthSubnetID,
		"subnetName":       "web",
		"vpcId":            vmPlanTestVpcID,
		"vpcName":          "prod",
		"l2SegmentId":      nil,
		"l2SegmentName":    nil,
	})
	for index, nic := range attached {
		rows = append(rows, map[string]any{
			"id":               nic.nicID,
			"networkInterface": index + 2,
			"isPrimary":        0,
			"macAddress":       fmt.Sprintf("fa:16:3e:00:00:%02d", index+2),
			"publicIp":         nil,
			"publicIpId":       nil,
			"privateIp":        nil,
			"world":            "l2",
			"networkName":      nil,
			"networkId":        nil,
			"cidrBlock":        nil,
			"gatewayIp":        nil,
			"networkType":      nil,
			"vpcSubnetId":      nil,
			"subnetName":       nil,
			"vpcId":            nil,
			"vpcName":          nil,
			"l2SegmentId":      nic.segmentID,
			"l2SegmentName":    "name of " + nic.segmentID,
		})
	}
	return map[string]any{"success": true, "message": "ok", "data": rows}
}

// vmSegmentListPlanTestConfig takes any number of segments, so a test can observe the
// attach loop. It asks for no retries, so a refused call fails on the first attempt.
func vmSegmentListPlanTestConfig(host, name string, segmentIDs ...string) string {
	quoted := make([]string, 0, len(segmentIDs))
	for _, segmentID := range segmentIDs {
		quoted = append(quoted, fmt.Sprintf("%q", segmentID))
	}

	return fmt.Sprintf(`
provider "gpcn" {
  host        = %q
  api_key     = "test-key"
  max_retries = 0
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  allocate_public_ip = false
  subnet_id          = %q
  l2_segment_ids     = [%s]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, strings.Join(quoted, ", "), vmPlanTestSshKeyID, vmPlanTestUsername)
}

// startVirtualMachineSegmentAttachMockServer refuses the post-create attach until the
// returned function heals it. The refusal is a 500, and the provider configuration asks
// for no retries. The attach therefore fails on the first call.
func startVirtualMachineSegmentAttachMockServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	attachFails := true
	status := virtualmachines.VMStatusRunning.String()
	var attached []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			attached = nil
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			refuse := attachFails
			if !refuse {
				segmentID, _ := body["l2SegmentId"].(string)
				attached = append(attached, segmentID)
			}
			mu.Unlock()
			if refuse {
				w.WriteHeader(http.StatusInternalServerError)
				testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "attach refused"})
				return
			}
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, vmSegmentNicsFromIds(current)))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			mu.Lock()
			attached = nil
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-4", "detach issued")
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	heal := func() {
		mu.Lock()
		attachFails = false
		mu.Unlock()
	}

	return server, heal
}

// A failed attach must not orphan the machine: it exists at the API, so it belongs in
// state. State must also name only the segments that attached, or no later plan can
// attach the rest. Terraform taints a resource whose create returned an error, so the
// last step plans a replacement. A machine absent from state would plan a bare create
// instead, with nothing to destroy.
func TestVirtualMachineResourcePlanCreateWritesStateWhenASegmentAttachFails(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, heal := startVirtualMachineSegmentAttachMockServer(t)

	config := vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-attach", vmPlanTestSegmentID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)attaching\s+` + vmPlanTestSegmentID + `\s+failed.*terraform\s+untaint`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "0"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "1"),
				),
			},
			{
				PreConfig: heal,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "1"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "2"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.1.l2_segment_id", vmPlanTestSegmentID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.1.world", "l2"),
				),
			},
		},
	})
}

// startVirtualMachineSegmentHotplugMockServer accepts every call and records the order in
// which they arrive. A test reads the order to learn whether the provider stopped the
// machine around the post-create attach. startSucceeds answers the start. A test then
// also observes what the provider does with a machine it cannot start again.
func startVirtualMachineSegmentHotplugMockServer(t *testing.T, hotplug int, startSucceeds bool) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()
	var attached []string
	var sequence []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sequence = append(sequence, r.Method+" "+r.URL.Path)
		mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			segmentID, _ := body["l2SegmentId"].(string)
			mu.Lock()
			attached = append(attached, segmentID)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, vmSegmentNicsFromIds(current)))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			mu.Lock()
			attached = nil
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-4", "detach issued")
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			if !startSucceeds {
				w.WriteHeader(http.StatusInternalServerError)
				testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "start refused"})
				return
			}
			mu.Lock()
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, vmPlanTestSizeID, hotplug))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sequence...)
	}

	return server, recorded
}

func indexOfRequest(sequence []string, request string) int {
	return slices.Index(sequence, request)
}

func lastIndexOfRequest(sequence []string, request string) int {
	for i := len(sequence) - 1; i >= 0; i-- {
		if sequence[i] == request {
			return i
		}
	}
	return -1
}

// GPCN refuses an add-NIC on a running machine whose image has no network hotplug. The
// provider therefore stops the machine around the whole loop and starts it after the
// last attach.
func TestVirtualMachineResourcePlanCreateStopsForASegmentAttachWithoutHotplug(t *testing.T) {
	tests := []struct {
		name          string
		hotplug       int
		segmentIDs    []string
		expectStopped bool
	}{
		// Three segments mean three attaches. A start after the first attach then
		// differs from a start after the last one.
		{"without hotplug the machine stops around the attach", 0, []string{vmPlanTestSegmentID, vmPlanTestSegmentID2, vmPlanTestSegmentID3}, true},
		{"with hotplug the machine stays running", 1, []string{vmPlanTestSegmentID, vmPlanTestSegmentID2}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortenVirtualMachinePolling(t)
			server, recorded := startVirtualMachineSegmentHotplugMockServer(t, tc.hotplug, true)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-hotplug", tc.segmentIDs...),
						Check: func(*terraform.State) error {
							sequence := recorded()
							attach := indexOfRequest(sequence, "POST "+vmPlanTestPath+"/network-interfaces")
							lastAttach := lastIndexOfRequest(sequence, "POST "+vmPlanTestPath+"/network-interfaces")
							stop := indexOfRequest(sequence, "POST "+vmPlanTestPath+"/stop")
							start := indexOfRequest(sequence, "POST "+vmPlanTestPath+"/start")
							if attach < 0 {
								return fmt.Errorf("expected an attach call, got %v", sequence)
							}
							if !tc.expectStopped {
								if stop >= 0 || start >= 0 {
									return fmt.Errorf("expected no stop or start, got %v", sequence)
								}
								return nil
							}
							if stop < 0 || stop > attach {
								return fmt.Errorf("expected a stop before the attach, got %v", sequence)
							}
							if start < 0 || start < lastAttach {
								return fmt.Errorf("expected a start after the last attach, got %v", sequence)
							}
							return nil
						},
					},
				},
			})
		})
	}
}

// startVirtualMachineSegmentUpdateMockServer keeps one interface row per attached
// segment and gives each a stable id. It records the segment verbs in the order they
// arrive. A test then reads what the provider changed and what it left alone. The
// second returned function attaches a segment and records no verb. It stands for work
// the platform completes before a read-back fails.
func startVirtualMachineSegmentUpdateMockServer(t *testing.T, hotplug int) (*httptest.Server, func(string), func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()
	nextNic := 2
	var attached []vmSegmentNic
	var events []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			segmentID, _ := body["l2SegmentId"].(string)
			mu.Lock()
			events = append(events, "attach "+segmentID)
			attached = append(attached, vmSegmentNic{nicID: fmt.Sprintf("nic-%d", nextNic), segmentID: segmentID})
			nextNic++
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			nicID := strings.TrimPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/")
			mu.Lock()
			events = append(events, "detach "+nicID)
			attached = slices.DeleteFunc(attached, func(nic vmSegmentNic) bool { return nic.nicID == nicID })
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-4", "detach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			current := append([]vmSegmentNic(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, current))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			events = append(events, "stop")
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			mu.Lock()
			events = append(events, "start")
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, vmPlanTestSizeID, hotplug))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	attachOutOfBand := func(segmentID string) {
		mu.Lock()
		defer mu.Unlock()
		attached = append(attached, vmSegmentNic{nicID: fmt.Sprintf("nic-%d", nextNic), segmentID: segmentID})
		nextNic++
	}

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, attachOutOfBand, recorded
}

// A segment list changes in place. The provider detaches the interface that carries the
// segment the configuration dropped and attaches one for the segment it gained. An image
// with network hotplug takes the change while the machine runs.
func TestVirtualMachineResourcePlanAttachesAndDetachesSegments(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, recorded := startVirtualMachineSegmentUpdateMockServer(t, 1)

	var afterCreate []string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-swap", vmPlanTestSegmentID),
				Check: func(*terraform.State) error {
					afterCreate = recorded()
					return nil
				},
			},
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-swap", vmPlanTestSegmentID2),
				Check: func(*terraform.State) error {
					update := recorded()[len(afterCreate):]
					want := []string{"detach nic-2", "attach " + vmPlanTestSegmentID2}
					if !slices.Equal(update, want) {
						return fmt.Errorf("expected %v, got %v", want, update)
					}
					return nil
				},
			},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "1"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.0", vmPlanTestSegmentID2),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "2"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.1.l2_segment_id", vmPlanTestSegmentID2),
				),
			},
		},
	})
}

// GPCN gives the interfaces of a machine no order, so a reordered segment list asks for
// nothing. A machine without network hotplug stops for a segment change. A stop for a
// list that carries the same segments costs the user the whole downtime.
func TestVirtualMachineResourcePlanReorderedSegmentsChangeNothing(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, recorded := startVirtualMachineSegmentUpdateMockServer(t, 0)

	afterCreate := 0

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-reorder", vmPlanTestSegmentID, vmPlanTestSegmentID2),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-reorder", vmPlanTestSegmentID2, vmPlanTestSegmentID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.0", vmPlanTestSegmentID2),
					func(*terraform.State) error {
						if reordered := recorded()[afterCreate:]; len(reordered) != 0 {
							return fmt.Errorf("expected the reorder to call nothing, got %v", reordered)
						}
						return nil
					},
				),
			},
		},
	})
}

// A failed read-back after an attach leaves state behind the machine. The next apply
// asks for a segment the machine already carries. The stop decision reads the live
// interfaces, so that retry costs the user no downtime.
func TestVirtualMachineResourcePlanSegmentRetryStopsNothing(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, attachOutOfBand, recorded := startVirtualMachineSegmentUpdateMockServer(t, 0)

	afterCreate := 0

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-retry", vmPlanTestSegmentID),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				PreConfig: func() { attachOutOfBand(vmPlanTestSegmentID2) },
				Config:    vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-retry", vmPlanTestSegmentID, vmPlanTestSegmentID2),
				Check: func(*terraform.State) error {
					if retry := recorded()[afterCreate:]; len(retry) != 0 {
						return fmt.Errorf("expected the retry to call nothing, got %v", retry)
					}
					return nil
				},
			},
		},
	})
}

// An image without network hotplug takes a new interface only on a stopped machine. The
// live interfaces still lack the segment here. The provider stops the machine, attaches
// it, and starts the machine again.
func TestVirtualMachineResourcePlanSegmentChangeStopsTheMachine(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _, recorded := startVirtualMachineSegmentUpdateMockServer(t, 0)

	afterCreate := 0

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-stop"),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-segment-stop", vmPlanTestSegmentID),
				Check: func(*terraform.State) error {
					update := recorded()[afterCreate:]
					want := []string{"stop", "attach " + vmPlanTestSegmentID, "start"}
					if !slices.Equal(update, want) {
						return fmt.Errorf("expected %v, got %v", want, update)
					}
					return nil
				},
			},
		},
	})
}

// vmStopDecisionModel carries the three attributes the stop decision reads.
func vmStopDecisionModel(hotplug bool, sizeId string, segmentIds ...string) virtualmachines.ResourceModel {
	elements := make([]attr.Value, 0, len(segmentIds))
	for _, segmentId := range segmentIds {
		elements = append(elements, types.StringValue(segmentId))
	}

	return virtualmachines.ResourceModel{
		NetworkHotplug: types.BoolValue(hotplug),
		SizeId:         types.StringValue(sizeId),
		L2SegmentIds:   types.ListValueMust(types.StringType, elements),
	}
}

// vmStopDecisionDetail reports the SKU the machine carries now.
func vmStopDecisionDetail(skuId string) *virtualmachines.ReadVirtualMachinesResponse {
	detail := &virtualmachines.ReadVirtualMachinesResponse{}
	detail.Data.Configuration.SkuId = skuId
	return detail
}

// vmStopDecisionInterfaces lists the birth interface and one L2 interface per segment
// the machine carries now.
func vmStopDecisionInterfaces(segmentIds ...string) []networks.ReadVirtualMachineNetworkDataResponseTF {
	interfaces := make([]networks.ReadVirtualMachineNetworkDataResponseTF, 0, 1+len(segmentIds))
	interfaces = append(interfaces, networks.ReadVirtualMachineNetworkDataResponseTF{
		ID:        types.StringValue("nic-1"),
		IsPrimary: types.BoolValue(true),
		World:     types.StringValue(networks.NicWorldVpc),
	})
	for index, segmentId := range segmentIds {
		interfaces = append(interfaces, networks.ReadVirtualMachineNetworkDataResponseTF{
			ID:          types.StringValue(fmt.Sprintf("nic-%d", index+2)),
			IsPrimary:   types.BoolValue(false),
			World:       types.StringValue(networks.NicWorldL2),
			L2SegmentID: types.StringValue(segmentId),
		})
	}

	return interfaces
}

// The machine already carries the planned segments, so the change is done. A stop for
// finished work costs the user the whole downtime.
func TestVirtualMachineStopDecisionSkipsAnAppliedSegmentChange(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID, vmPlanTestSegmentID2)

	live := vmStopDecisionDetail(vmPlanTestSizeID)
	liveInterfaces := vmStopDecisionInterfaces(vmPlanTestSegmentID, vmPlanTestSegmentID2)

	if determineIfVMNeedsStopped(state, plan, live, liveInterfaces) {
		t.Error("Expected no stop for a segment list the machine already carries")
	}
}

// GPCN refuses an add-NIC on a running machine without network hotplug. A segment the
// live interfaces lack therefore needs a stop.
func TestVirtualMachineStopDecisionStopsForASegmentTheMachineLacks(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID, vmPlanTestSegmentID2)

	live := vmStopDecisionDetail(vmPlanTestSizeID)
	liveInterfaces := vmStopDecisionInterfaces(vmPlanTestSegmentID)

	if !determineIfVMNeedsStopped(state, plan, live, liveInterfaces) {
		t.Error("Expected a stop for a segment the machine lacks")
	}
}

// The machine already carries the planned SKU, so the resize is done. State lags the
// machine after a failed read-back, and state is not the question.
func TestVirtualMachineStopDecisionSkipsAnAppliedResize(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID2)

	live := vmStopDecisionDetail(vmPlanTestSizeID2)

	if determineIfVMNeedsStopped(state, plan, live, vmStopDecisionInterfaces()) {
		t.Error("Expected no stop for a SKU the machine already carries")
	}
}

// GPCN resizes a stopped machine only, so a SKU the machine lacks needs a stop.
func TestVirtualMachineStopDecisionStopsForAResize(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID2)

	live := vmStopDecisionDetail(vmPlanTestSizeID)

	if !determineIfVMNeedsStopped(state, plan, live, vmStopDecisionInterfaces()) {
		t.Error("Expected a stop for a SKU the machine lacks")
	}
}

// An image with network hotplug takes every change while the machine runs.
func TestVirtualMachineStopDecisionSkipsAMachineWithHotplug(t *testing.T) {
	state := vmStopDecisionModel(true, vmPlanTestSizeID)
	plan := vmStopDecisionModel(true, vmPlanTestSizeID2, vmPlanTestSegmentID)

	live := vmStopDecisionDetail(vmPlanTestSizeID)

	if determineIfVMNeedsStopped(state, plan, live, vmStopDecisionInterfaces()) {
		t.Error("Expected no stop for a machine whose image takes network hotplug")
	}
}

// DEV sends a null skuId for a machine whose SKU it cannot resolve. The provider
// reads that null as an empty string. Live drift alone must never stop a machine. A
// rename would otherwise stop and start such a machine on every apply.
func TestVirtualMachineStopDecisionSkipsARenameOfAnUnresolvedSku(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID)

	live := vmStopDecisionDetail("")

	if determineIfVMNeedsStopped(state, plan, live, vmStopDecisionInterfaces()) {
		t.Error("Expected no stop for a rename of a machine whose SKU is unresolved")
	}
}

// A segment the platform attached out of band leaves the live set ahead of state. The
// configuration asks for no segment change here, so the machine needs no stop.
func TestVirtualMachineStopDecisionSkipsARenameOfADriftedSegmentSet(t *testing.T) {
	state := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID)
	plan := vmStopDecisionModel(false, vmPlanTestSizeID, vmPlanTestSegmentID)

	live := vmStopDecisionDetail(vmPlanTestSizeID)
	liveInterfaces := vmStopDecisionInterfaces(vmPlanTestSegmentID, vmPlanTestSegmentID2)

	if determineIfVMNeedsStopped(state, plan, live, liveInterfaces) {
		t.Error("Expected no stop for a rename of a machine whose live segments drifted")
	}
}

// vmPlanTestReadBodyWithUnresolvedSku reports a machine whose SKU GPCN cannot resolve.
// DEV sends a null skuId and placeholder values for such a machine.
func vmPlanTestReadBodyWithUnresolvedSku(name, status string, hotplug int) map[string]any {
	body := vmPlanTestReadBodyWithHotplug(name, status, "", hotplug)
	configuration := body["data"].(map[string]any)["configuration"].(map[string]any)
	configuration["skuId"] = nil
	configuration["skuCode"] = nil
	configuration["name"] = "Unknown"
	configuration["cpu"] = 0
	configuration["ram"] = 0
	configuration["disk"] = 0
	configuration["degradedReason"] = "sku_retired"
	return body
}

// startVirtualMachineUnresolvedSkuMockServer reports a machine whose SKU GPCN cannot
// resolve and an image that takes no network hotplug. The returned function lists the
// stops and the starts the provider issues.
func startVirtualMachineUnresolvedSkuMockServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()
	var events []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, nil))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			events = append(events, "stop")
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			mu.Lock()
			events = append(events, "start")
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithUnresolvedSku(currentName, currentStatus, 0))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, recorded
}

// A machine whose SKU GPCN cannot resolve reports a null skuId on every read. The rename
// asks for no size change, so the machine keeps running through it.
func TestVirtualMachineResourcePlanRenameOfAnUnresolvedSkuStopsNothing(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachineUnresolvedSkuMockServer(t)

	afterCreate := 0

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-unresolved-sku"),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-unresolved-sku-renamed"),
				Check: func(*terraform.State) error {
					if rename := recorded()[afterCreate:]; len(rename) != 0 {
						return fmt.Errorf("expected the rename to stop and start nothing, got %v", rename)
					}
					return nil
				},
			},
		},
	})
}

// startVirtualMachineSegmentRefusedMockServer counts the starts the provider issues. The
// machine reports the given network hotplug value, so a test chooses whether an update
// stops it first. The segment attach is refused unless getFailsAfterStop is set, because
// the read-back is only reachable past a successful attach. That flag then makes the
// first GET after the attach answer 500. startSucceeds answers the start.
func startVirtualMachineSegmentRefusedMockServer(t *testing.T, startSucceeds bool, hotplug int, getFailsAfterStop bool) (*httptest.Server, func() int) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()
	startCount := 0
	attachDone := false
	readBackFailed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			if getFailsAfterStop {
				mu.Lock()
				attachDone = true
				mu.Unlock()
				testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "attach refused"})
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, nil))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			mu.Lock()
			startCount++
			if startSucceeds {
				status = virtualmachines.VMStatusRunning.String()
			}
			mu.Unlock()
			if !startSucceeds {
				w.WriteHeader(http.StatusInternalServerError)
				testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "start refused"})
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			refuseReadBack := attachDone && !readBackFailed
			readBackFailed = readBackFailed || refuseReadBack
			mu.Unlock()
			if refuseReadBack {
				w.WriteHeader(http.StatusInternalServerError)
				testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "read-back refused"})
				return
			}
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, vmPlanTestSizeID, hotplug))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	starts := func() int {
		mu.Lock()
		defer mu.Unlock()
		return startCount
	}

	return server, starts
}

// vmPlanTestAttachErrorPattern matches the attach refusal after Terraform wraps it.
var vmPlanTestAttachErrorPattern = regexp.MustCompile(`(?s)Error\s+updating\s+network\s+interfaces`)

// vmPlanTestLeftStoppedPattern matches the left-stopped summary after Terraform wraps it.
var vmPlanTestLeftStoppedPattern = regexp.MustCompile(`(?s)Virtual\s+machine\s+left\s+stopped`)

// vmPlanTestRetrieveErrorPattern matches a refused read of the machine after Terraform
// wraps it.
var vmPlanTestRetrieveErrorPattern = regexp.MustCompile(`(?s)Retrieving\s+information\s+about\s+the\s+Virtual\s+Machine\s+failed`)

// vmPlanTestInterfaceReadErrorPattern matches a refused read of the interfaces after
// Terraform wraps it.
var vmPlanTestInterfaceReadErrorPattern = regexp.MustCompile(`(?s)Error\s+retrieving\s+network\s+interfaces`)

// The update stops the machine and the segment attach then fails. The provider starts
// the machine again, so the user reads the attach error alone. The step sets no
// ExpectError, because ErrorCheck never runs for a step that sets one. A flag records
// that the check ran: an apply that stops failing would otherwise assert nothing.
func TestVirtualMachineResourcePlanStartsAgainWhenAnUpdateStepFailsAfterTheStop(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, startCount := startVirtualMachineSegmentRefusedMockServer(t, true, 0, false)

	errorCheckRan := false

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		ErrorCheck: func(err error) error {
			errorCheckRan = true
			if !vmPlanTestAttachErrorPattern.MatchString(err.Error()) {
				t.Errorf("Expected the attach error, got '%s'", err.Error())
			}
			if vmPlanTestLeftStoppedPattern.MatchString(err.Error()) {
				t.Errorf("Expected no '%s' diagnostic, got '%s'", virtualmachines.ErrSummaryVMLeftStopped, err.Error())
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-restart-after-failure"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-restart-after-failure", vmPlanTestSegmentID),
			},
		},
	})

	if !errorCheckRan {
		t.Error("Expected the apply to fail and ErrorCheck to run")
	}
	if starts := startCount(); starts != 1 {
		t.Errorf("Expected exactly 1 start, got %d", starts)
	}
}

// The update stops the machine, the attach fails, and the start fails too. The user
// reads both errors, so the machine that stays stopped is never a silent one. The
// refresh step proves the mid-update path records nothing, so the next apply retries.
func TestVirtualMachineResourcePlanReportsLeftStoppedWhenTheStartAlsoFails(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, startCount := startVirtualMachineSegmentRefusedMockServer(t, false, 0, false)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-left-stopped-update"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-left-stopped-update", vmPlanTestSegmentID),
				ExpectError: regexp.MustCompile(`(?s)Error\s+updating\s+network\s+interfaces.*Virtual\s+machine\s+left\s+stopped.*did\s+not\s+start\s+again.*then\s+run\s+terraform\s+plan\s+and\s+check\s+the\s+proposed\s+changes\s+before\s+applying\.`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "0"),
				),
			},
		},
	})

	if starts := startCount(); starts != 1 {
		t.Errorf("Expected exactly 1 start, got %d", starts)
	}
}

// Network hotplug keeps the machine running through the attach. A failed attach leaves
// nothing to start. A start would then reboot a machine the provider never stopped.
func TestVirtualMachineResourcePlanDoesNotStartAMachineItNeverStopped(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, startCount := startVirtualMachineSegmentRefusedMockServer(t, true, 1, false)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-never-stopped"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-never-stopped", vmPlanTestSegmentID),
				ExpectError: vmPlanTestAttachErrorPattern,
			},
		},
	})

	if starts := startCount(); starts != 0 {
		t.Errorf("Expected no start, got %d", starts)
	}
}

// The read-back after the update is the last step that can fail before the start.
// It runs inside the runner, so a refused GET also starts the machine again.
func TestVirtualMachineResourcePlanStartsAgainWhenTheReadBackFails(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, startCount := startVirtualMachineSegmentRefusedMockServer(t, true, 0, true)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-read-back-fails"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-read-back-fails", vmPlanTestSegmentID),
				ExpectError: vmPlanTestRetrieveErrorPattern,
			},
		},
	})

	if starts := startCount(); starts != 1 {
		t.Errorf("Expected exactly 1 start, got %d", starts)
	}
}

// vmPlanTestReadsBeforeAnUpdate counts the reads Terraform takes of the machine before
// it applies a step. The plan takes one. The read the update takes for its own decision
// is the one after it.
const vmPlanTestReadsBeforeAnUpdate = 1

// startVirtualMachineHoistedReadMockServer refuses one read of the update. The update
// reads the machine and its interfaces before it decides on a stop. The returned
// function arms the refusal between the steps, so the create never meets it. The
// refusal answers one read only. refuseInterfaces chooses which of the two reads answers
// 500. The image takes no network hotplug, so a segment change stops a machine that
// reads back.
func startVirtualMachineHoistedReadMockServer(t *testing.T, refuseInterfaces bool) (*httptest.Server, func(), func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	status := virtualmachines.VMStatusRunning.String()
	armed := false
	reads := 0
	refused := false
	var events []string

	// refuseThisReadLocked answers the caller that holds the lock. It counts the reads of
	// the refresh past and then answers true once.
	refuseThisReadLocked := func() bool {
		if !armed || refused {
			return false
		}
		reads++
		if reads <= vmPlanTestReadsBeforeAnUpdate {
			return false
		}
		refused = true
		return true
	}

	refuse := func(w http.ResponseWriter, message string) {
		w.WriteHeader(http.StatusInternalServerError)
		testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": message})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			segmentID, _ := body["l2SegmentId"].(string)
			mu.Lock()
			events = append(events, "attach "+segmentID)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet := birthSubnetID
			refuseRead := refuseInterfaces && refuseThisReadLocked()
			mu.Unlock()
			if refuseRead {
				refuse(w, "interfaces refused")
				return
			}
			testutil.WriteJSONResponse(w, vmSegmentPlanTestNetworkInterfacesBody(currentSubnet, nil))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			events = append(events, "stop")
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			mu.Lock()
			events = append(events, "start")
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			refuseRead := !refuseInterfaces && refuseThisReadLocked()
			mu.Unlock()
			if refuseRead {
				refuse(w, "detail refused")
				return
			}
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, vmPlanTestSizeID, 0))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	arm := func() {
		mu.Lock()
		armed = true
		mu.Unlock()
	}

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, arm, recorded
}

// The update reads the machine before it decides on a stop. A refused read leaves the
// provider without the live SKU, so it fails the update and stops nothing.
func TestVirtualMachineResourcePlanRefusedDetailReadFailsTheUpdateWithoutAStop(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, arm, recorded := startVirtualMachineHoistedReadMockServer(t, false)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-refused-detail"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig:   arm,
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-refused-detail", vmPlanTestSegmentID),
				ExpectError: vmPlanTestRetrieveErrorPattern,
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(*terraform.State) error {
					if events := recorded(); len(events) != 0 {
						return fmt.Errorf("expected the refused read to call nothing, got %v", events)
					}
					return nil
				},
			},
		},
	})
}

// The update reads the interfaces of the machine before it decides on a stop. A refused
// read leaves the provider without the live segments, so it fails the update and stops
// nothing.
func TestVirtualMachineResourcePlanRefusedInterfaceReadFailsTheUpdateWithoutAStop(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, arm, recorded := startVirtualMachineHoistedReadMockServer(t, true)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-refused-interfaces"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				PreConfig:   arm,
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-refused-interfaces", vmPlanTestSegmentID),
				ExpectError: vmPlanTestInterfaceReadErrorPattern,
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(*terraform.State) error {
					if events := recorded(); len(events) != 0 {
						return fmt.Errorf("expected the refused read to call nothing, got %v", events)
					}
					return nil
				},
			},
		},
	})
}

const (
	vmPlanTestAcquiredIpID      = "acquired-ip-1"
	vmPlanTestAcquiredIpAddress = "203.0.113.10"
	vmPlanTestHeldIpID          = "held-ip-1"
	vmPlanTestHeldIpAddress     = "203.0.113.20"
)

// vmPublicIpPlanTestInterfacesBody reports the birth interface with whatever address is
// bound to it. On a VPC interface publicIpId is the address row id, not a provider id.
func vmPublicIpPlanTestInterfacesBody(addressID, address string) map[string]any {
	body := vmSegmentPlanTestNetworkInterfacesBody(vmPlanTestSubnetID, nil)
	row := body["data"].([]map[string]any)[0]
	if addressID == "" {
		return body
	}
	row["publicIp"] = address
	row["publicIpId"] = addressID
	return body
}

// startVirtualMachinePublicIpMockServer serves the VPC address verbs and records them in
// order. The legacy per-NIC routes answer nothing but a test failure. GPCN refuses them
// on a VPC interface, so the provider must never reach for one. A create that names an
// address binds it, and the image list answers the import. refuseReadAfterChange makes
// the first read of the machine after an attach or a rename answer 500. That read is
// the read-back of the update. It answers one read only, so the destroy still reaches
// the machine.
func startVirtualMachinePublicIpMockServer(t *testing.T, refuseReadAfterChange bool) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	boundID := ""
	boundAddress := ""
	refuseNextRead := false
	var events []string

	addressesPath := "/v1/resource/vpcs/" + vmPlanTestVpcID + "/public-ips"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			status = virtualmachines.VMStatusRunning.String()
			if acquire, ok := body["acquirePublicIp"].(bool); ok && acquire {
				boundID, boundAddress = vmPlanTestAcquiredIpID, vmPlanTestAcquiredIpAddress
			}
			if held, ok := body["publicIpId"].(string); ok && held != "" {
				boundID, boundAddress = held, vmPlanTestHeldIpAddress
			}
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-images":
			testutil.WriteJSONResponse(w, vmPlanTestImagesBody())
		case r.Method == http.MethodPost && r.URL.Path == addressesPath:
			mu.Lock()
			events = append(events, "acquire")
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"publicIpId": vmPlanTestAcquiredIpID, "jobId": "job-ip"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attach") && strings.HasPrefix(r.URL.Path, addressesPath+"/"):
			addressID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, addressesPath+"/"), "/attach")
			nicID, _ := testutil.ReadRequestBody(r)["nicId"].(string)
			mu.Lock()
			events = append(events, "attach "+addressID+" "+nicID)
			boundID = addressID
			boundAddress = vmPlanTestAcquiredIpAddress
			if addressID == vmPlanTestHeldIpID {
				boundAddress = vmPlanTestHeldIpAddress
			}
			refuseNextRead = refuseReadAfterChange
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-ip", "attach issued")
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/detach") && strings.HasPrefix(r.URL.Path, addressesPath+"/"):
			addressID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, addressesPath+"/"), "/detach")
			mu.Lock()
			events = append(events, "detach "+addressID)
			boundID = ""
			boundAddress = ""
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-ip", "detach issued")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, addressesPath+"/"):
			mu.Lock()
			events = append(events, "release "+strings.TrimPrefix(r.URL.Path, addressesPath+"/"))
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-ip", "release issued")
		case strings.HasSuffix(r.URL.Path, "/public-ip") && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			mu.Lock()
			events = append(events, "legacy "+r.Method)
			mu.Unlock()
			t.Errorf("Expected no legacy per-interface public IP call, got %s %s", r.Method, r.URL.Path)
			testutil.HandleCreateJobResponse(w, "job-legacy", "legacy issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentID, currentAddress := boundID, boundAddress
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPublicIpPlanTestInterfacesBody(currentID, currentAddress))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			refused := refuseNextRead
			refuseNextRead = false
			mu.Unlock()
			if refused {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			refuseNextRead = refuseReadAfterChange
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, recorded
}

// vmPublicIpPlanTestConfig writes the address attributes a test toggles. An empty
// publicIpID leaves public_ip_id out of the configuration altogether.
func vmPublicIpPlanTestConfig(host, name string, allocate bool, publicIpID string) string {
	held := ""
	if publicIpID != "" {
		held = fmt.Sprintf("\n  public_ip_id       = %q", publicIpID)
	}

	return fmt.Sprintf(`
provider "gpcn" {
  host        = %q
  api_key     = "test-key"
  max_retries = 0
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  subnet_id          = %q
  allocate_public_ip = %t%s
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, allocate, held, vmPlanTestSshKeyID, vmPlanTestUsername)
}

// GPCN answers 409 when the legacy per-interface allocate addresses a VPC interface. An
// address the machine asks for is therefore acquired on the VPC and attached to the
// interface. Giving it up detaches it and then releases it, because Terraform acquired
// it.
func TestVirtualMachineResourcePlanTogglesPublicIpViaVpcVerbs(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachinePublicIpMockServer(t, false)

	var afterCreate, afterAcquire int

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-address", false, ""),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-address", true, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestAcquiredIpAddress),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.public_ip_id", vmPlanTestAcquiredIpID),
					func(*terraform.State) error {
						afterAcquire = len(recorded())
						acquire := recorded()[afterCreate:]
						want := []string{"acquire", "attach " + vmPlanTestAcquiredIpID + " nic-1"}
						if !slices.Equal(acquire, want) {
							return fmt.Errorf("expected %v, got %v", want, acquire)
						}
						return nil
					},
				),
			},
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-address", false, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "public_ip"),
					func(*terraform.State) error {
						release := recorded()[afterAcquire:]
						want := []string{"detach " + vmPlanTestAcquiredIpID, "release " + vmPlanTestAcquiredIpID}
						if !slices.Equal(release, want) {
							return fmt.Errorf("expected %v, got %v", want, release)
						}
						return nil
					},
				),
			},
		},
	})
}

// A held address belongs to the operator, not to the machine. Naming one attaches it,
// and dropping the name detaches it. Terraform never releases an address it did not
// acquire.
func TestVirtualMachineResourcePlanAttachesAndDetachesAHeldAddress(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachinePublicIpMockServer(t, false)

	var afterCreate, afterAttach int

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-held-address", false, ""),
				Check: func(*terraform.State) error {
					afterCreate = len(recorded())
					return nil
				},
			},
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-held-address", false, vmPlanTestHeldIpID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestHeldIpAddress),
					func(*terraform.State) error {
						afterAttach = len(recorded())
						attach := recorded()[afterCreate:]
						want := []string{"attach " + vmPlanTestHeldIpID + " nic-1"}
						if !slices.Equal(attach, want) {
							return fmt.Errorf("expected %v, got %v", want, attach)
						}
						return nil
					},
				),
			},
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-held-address", false, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "public_ip"),
					func(*terraform.State) error {
						detach := recorded()[afterAttach:]
						want := []string{"detach " + vmPlanTestHeldIpID}
						if !slices.Equal(detach, want) {
							return fmt.Errorf("expected %v, got %v", want, detach)
						}
						return nil
					},
				),
			},
		},
	})
}

// GPCN reports one address row whether Terraform acquired the address or the operator
// holds it. An import therefore records what it finds as a held address. An inferred
// allocate_public_ip would release an address the operator owns on the next destroy.
func TestVirtualMachineResourcePlanImportRecordsAHeldAddress(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _ := startVirtualMachinePublicIpMockServer(t, false)

	config := vmPublicIpPlanTestConfig(server.URL, "vm-plan-import-address", false, vmPlanTestHeldIpID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestHeldIpAddress),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip_id", vmPlanTestHeldIpID),
				),
			},
			{
				ResourceName:      gpcnVirtualMachineTest,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					attributes := states[0].Attributes
					if attributes["allocate_public_ip"] != "false" {
						return fmt.Errorf("expected allocate_public_ip 'false', got '%s'", attributes["allocate_public_ip"])
					}
					if attributes["public_ip_id"] != vmPlanTestHeldIpID {
						return fmt.Errorf("expected public_ip_id '%s', got '%s'", vmPlanTestHeldIpID, attributes["public_ip_id"])
					}
					return nil
				},
			},
		},
	})
}

// vmPlanTestAcquiredAddressReadBackPattern matches the orphan report that follows a
// refused read-back, after Terraform wraps it.
var vmPlanTestAcquiredAddressReadBackPattern = regexp.MustCompile(
	`(?s)public\s+IP\s+` + vmPlanTestAcquiredIpID + `\s+was\s+acquired\s+for\s+virtual\s+machine\s+` +
		vmPlanTestID + `\s+but\s+reading\s+the\s+machine\s+back\s+failed`)

// The acquire and the attach both succeed, and the read-back then fails. State keeps
// allocate_public_ip false, so no destroy gives the address back. The diagnostic is the
// only record of it, so it names the id.
func TestVirtualMachineResourcePlanNamesTheAddressWhenTheReadBackFails(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachinePublicIpMockServer(t, true)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-read-back-fails", false, ""),
			},
			{
				Config:      vmPublicIpPlanTestConfig(server.URL, "vm-plan-read-back-fails", true, ""),
				ExpectError: vmPlanTestAcquiredAddressReadBackPattern,
			},
		},
	})

	want := []string{"acquire", "attach " + vmPlanTestAcquiredIpID + " nic-1"}
	if verbs := recorded(); !slices.Equal(verbs, want) {
		t.Errorf("Expected %v, got %v", want, verbs)
	}
}

// vmPlanTestOrphanSentencePattern matches the orphan report whatever address it names.
// A report of an address the change never acquired names an empty id.
var vmPlanTestOrphanSentencePattern = regexp.MustCompile(`(?s)was\s+acquired\s+for\s+virtual\s+machine`)

// vmPlanTestReadBackDetailPattern matches the read-back arm alone. The hoisted read
// before the update reports the same summary with the bare error.
var vmPlanTestReadBackDetailPattern = regexp.MustCompile(`(?s)import\s+the\s+id\s+to\s+repair\s+the\s+state`)

// A rename acquires no address, so its failed read-back reports the retrieve error
// alone. An orphan sentence here sends the operator to the portal for an address that
// does not exist. The step sets no ExpectError, because ErrorCheck never runs for a
// step that sets one. A flag records that the check ran.
func TestVirtualMachineResourcePlanRenameReadBackFailureNamesNoAddress(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachinePublicIpMockServer(t, true)

	errorCheckRan := false

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		ErrorCheck: func(err error) error {
			errorCheckRan = true
			if !vmPlanTestRetrieveErrorPattern.MatchString(err.Error()) {
				t.Errorf("Expected the retrieve error, got '%s'", err.Error())
			}
			if !vmPlanTestReadBackDetailPattern.MatchString(err.Error()) {
				t.Errorf("Expected the read-back detail, got '%s'", err.Error())
			}
			if vmPlanTestOrphanSentencePattern.MatchString(err.Error()) {
				t.Errorf("Expected no orphan sentence, got '%s'", err.Error())
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-rename-read-back", false, ""),
			},
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-rename-read-back-again", false, ""),
			},
		},
	})

	if !errorCheckRan {
		t.Error("Expected the apply to fail and ErrorCheck to run")
	}
	if verbs := recorded(); len(verbs) != 0 {
		t.Errorf("Expected no address verb, got %v", verbs)
	}
}

// vmCreateOnSubnetInterfacesBody reports the birth interface, its acquired address and
// one interface per attached segment. An empty addressID leaves the address columns
// null, as GPCN does for an interface with no address.
func vmCreateOnSubnetInterfacesBody(birthSubnetID, addressID string, attached []string) map[string]any {
	body := vmSegmentPlanTestNetworkInterfacesBody(birthSubnetID, vmSegmentNicsFromIds(attached))
	if addressID == "" {
		return body
	}
	row := body["data"].([]map[string]any)[0]
	row["publicIp"] = vmPlanTestAcquiredIpAddress
	row["publicIpId"] = addressID
	return body
}

// vmCreateOnSubnetPlanTestConfig asks for a machine on a subnet, with two segments and
// an acquired address. One create then exercises all three networking attributes.
func vmCreateOnSubnetPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host        = %q
  api_key     = "test-key"
  max_retries = 0
}

resource "gpcn_virtualmachine" "test" {
  name               = %q
  datacenter_id      = %q
  size_id            = %q
  image_id           = %q
  subnet_id          = %q
  l2_segment_ids     = [%q, %q]
  allocate_public_ip = true
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestSubnetID, vmPlanTestSegmentID, vmPlanTestSegmentID2, vmPlanTestSshKeyID, vmPlanTestUsername)
}

// startVirtualMachineCreateOnSubnetMockServer keeps the create body and the segments the
// attach loop asked for. The image takes network hotplug, so the loop needs no stop.
func startVirtualMachineCreateOnSubnetMockServer(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthSubnetID := ""
	boundID := ""
	status := virtualmachines.VMStatusRunning.String()
	var attached []string
	var createBody map[string]any

	addressesPath := "/v1/resource/vpcs/" + vmPlanTestVpcID + "/public-ips"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			createBody = body
			name, _ = body["name"].(string)
			birthSubnetID, _ = body["subnetId"].(string)
			if acquire, ok := body["acquirePublicIp"].(bool); ok && acquire {
				boundID = vmPlanTestAcquiredIpID
			}
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			segmentID, _ := testutil.ReadRequestBody(r)["l2SegmentId"].(string)
			mu.Lock()
			attached = append(attached, segmentID)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			testutil.HandleCreateJobResponse(w, "job-4", "detach issued")
		case strings.HasPrefix(r.URL.Path, addressesPath):
			testutil.HandleCreateJobResponse(w, "job-ip", "address issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentSubnet, currentAddress := birthSubnetID, boundID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmCreateOnSubnetInterfacesBody(currentSubnet, currentAddress, current))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, vmPlanTestSizeID, 1))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return createBody
	}

	return server, recorded
}

// The three networking attributes reach GPCN by three different routes. subnet_id goes
// in the create body. l2_segment_ids goes through the attach loop after it.
// allocate_public_ip is a flag the API never reports back. One create pins all three in
// state together.
func TestVirtualMachineResourcePlanCreateOnSubnet(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, createBody := startVirtualMachineCreateOnSubnetMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmCreateOnSubnetPlanTestConfig(server.URL, "vm-plan-create-on-subnet"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "subnet_id", vmPlanTestSubnetID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "2"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.0", vmPlanTestSegmentID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.1", vmPlanTestSegmentID2),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "allocate_public_ip", "true"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestAcquiredIpAddress),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "3"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.vpc_subnet_id", vmPlanTestSubnetID),
					func(*terraform.State) error {
						body := createBody()
						if got, _ := body["subnetId"].(string); got != vmPlanTestSubnetID {
							return fmt.Errorf("expected the create body to name subnet %q, got %q", vmPlanTestSubnetID, got)
						}
						if acquire, _ := body["acquirePublicIp"].(bool); !acquire {
							return fmt.Errorf("expected the create body to ask for an address, got %v", body["acquirePublicIp"])
						}
						if _, named := body["l2SegmentIds"]; named {
							return fmt.Errorf("expected no segment key in the create body, got %v", body)
						}
						return nil
					},
				),
			},
		},
	})
}

// startVirtualMachineCarriedAddressMockServer reports an address on the primary
// interface from the first read. It stands for a machine whose acquisition succeeded
// and whose read-back did not, so state lags the platform. Every address verb is
// recorded, because the platform refuses a second address on that interface.
func startVirtualMachineCarriedAddressMockServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	var events []string

	addressesPath := "/v1/resource/vpcs/" + vmPlanTestVpcID + "/public-ips"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case strings.HasPrefix(r.URL.Path, addressesPath):
			mu.Lock()
			events = append(events, r.Method+" "+r.URL.Path)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-ip", "address issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			testutil.WriteJSONResponse(w, vmPublicIpPlanTestInterfacesBody(vmPlanTestAcquiredIpID, vmPlanTestAcquiredIpAddress))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	return server, recorded
}

// vmPlanTestForeignAddressPattern matches the refusal of a carried address after
// Terraform wraps it.
var vmPlanTestForeignAddressPattern = regexp.MustCompile(
	`(?s)already\s+carries\s+public\s+IP\s+` + vmPlanTestAcquiredIpID +
		`.*name\s+it\s+in\s+public_ip_id\s+or\s+detach\s+it`)

// A machine can carry an address Terraform did not acquire. The provider cannot tell a
// held one from the leftover of a failed read-back. Adopting it makes the next destroy
// release an address gpcn_vpc_public_ip owns, so the update refuses.
func TestVirtualMachineResourcePlanRefusesAnAddressItDidNotAcquire(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachineCarriedAddressMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPublicIpPlanTestConfig(server.URL, "vm-plan-carried-address", false, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "allocate_public_ip", "false"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestAcquiredIpAddress),
				),
			},
			{
				Config:      vmPublicIpPlanTestConfig(server.URL, "vm-plan-carried-address", true, ""),
				ExpectError: vmPlanTestForeignAddressPattern,
			},
		},
	})

	if verbs := recorded(); len(verbs) != 0 {
		t.Errorf("Expected no address verb, got %v", verbs)
	}
}

// vmLegacyPlanTestInterfacesBody reports a primary interface on a legacy network. A
// machine created by 1.3.0 has one, and its address is not a VPC address.
func vmLegacyPlanTestInterfacesBody(addressID, address string) map[string]any {
	body := vmPublicIpPlanTestInterfacesBody(addressID, address)
	row := body["data"].([]map[string]any)[0]
	row["world"] = "legacy"
	row["vpcId"] = nil
	row["vpcName"] = nil
	row["vpcSubnetId"] = nil
	row["subnetName"] = nil
	row["networkId"] = "net-1"
	row["networkName"] = "legacy-net"
	row["networkType"] = "standard"
	return body
}

// vmDestroyBodyMock arms the destroy-body mock. A legacy primary reports the birth
// interface on a legacy network. A failing read-back refuses the interface list until
// the destroy stops the machine, so state holds no list at all.
type vmDestroyBodyMock struct {
	legacyPrimary bool
	readBackFails bool
}

// startVirtualMachineDestroyBodyMockServer keeps whichever address the create asked for
// and records the body of the delete. A test then reads what the destroy asked GPCN to
// do with that address.
func startVirtualMachineDestroyBodyMockServer(t *testing.T, arm vmDestroyBodyMock) (*httptest.Server, func() (string, bool)) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	boundID := ""
	boundAddress := ""
	deleteBody := ""
	deleted := false

	addressesPath := "/v1/resource/vpcs/" + vmPlanTestVpcID + "/public-ips"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			status = virtualmachines.VMStatusRunning.String()
			if acquire, ok := body["acquirePublicIp"].(bool); ok && acquire {
				boundID, boundAddress = vmPlanTestAcquiredIpID, vmPlanTestAcquiredIpAddress
			}
			if held, ok := body["publicIpId"].(string); ok && held != "" {
				boundID, boundAddress = held, vmPlanTestHeldIpAddress
			}
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case strings.HasPrefix(r.URL.Path, addressesPath):
			t.Errorf("Expected no address verb during the destroy, got %s %s", r.Method, r.URL.Path)
			testutil.HandleCreateJobResponse(w, "job-ip", "address issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentID, currentAddress, currentStatus := boundID, boundAddress, status
			mu.Unlock()
			// The destroy stops the machine before it reads the list. An armed read-back
			// failure therefore refuses every read but that one.
			if arm.readBackFails && currentStatus != virtualmachines.VMStatusShutoff.String() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if arm.legacyPrimary {
				testutil.WriteJSONResponse(w, vmLegacyPlanTestInterfacesBody(currentID, currentAddress))
				return
			}
			testutil.WriteJSONResponse(w, vmPublicIpPlanTestInterfacesBody(currentID, currentAddress))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			deleteBody = string(raw)
			deleted = true
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	recorded := func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return deleteBody, deleted
	}

	return server, recorded
}

// A destroy gives back only what Terraform acquired. An address the operator holds
// survives the machine. The delete that carries one names no disposition at all, and
// GPCN keeps it. A legacy machine has no VPC address, and the release key costs it the
// vpc-public-ip:delete permission for nothing.
func TestVirtualMachineResourcePlanDestroyReleasesAcquiredIp(t *testing.T) {
	tests := []struct {
		name          string
		allocate      bool
		publicIpID    string
		legacyPrimary bool
		wantBody      string
	}{
		{"an acquired address is released", true, "", false, `{"releasePublicIps":true}`},
		{"a held address is kept", false, vmPlanTestHeldIpID, false, ""},
		{"a legacy machine asks for nothing", true, "", true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortenVirtualMachinePolling(t)
			server, recorded := startVirtualMachineDestroyBodyMockServer(t, vmDestroyBodyMock{legacyPrimary: tc.legacyPrimary})

			config := vmPublicIpPlanTestConfig(server.URL, "vm-plan-destroy-address", tc.allocate, tc.publicIpID)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config},
					{Config: config, Destroy: true},
				},
			})

			body, deleted := recorded()
			if !deleted {
				t.Fatal("Expected the machine to be deleted")
			}
			if body != tc.wantBody {
				t.Errorf("Expected the delete body '%s', got '%s'", tc.wantBody, body)
			}
		})
	}
}

// A read-back that fails leaves state with no interface list. The destroy must still
// give back an address Terraform acquired. The live list the destroy fetches before it
// asks names the world, and it is the only source.
func TestVirtualMachineResourcePlanDestroyReadsTheLiveInterfaceWorld(t *testing.T) {
	tests := []struct {
		name          string
		legacyPrimary bool
		wantBody      string
	}{
		{"a live VPC primary releases the address", false, `{"releasePublicIps":true}`},
		{"a live legacy primary asks for nothing", true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortenVirtualMachinePolling(t)
			server, recorded := startVirtualMachineDestroyBodyMockServer(t, vmDestroyBodyMock{
				legacyPrimary: tc.legacyPrimary,
				readBackFails: true,
			})

			config := vmPublicIpPlanTestConfig(server.URL, "vm-plan-destroy-live-world", true, "")

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: config,
						Check:  resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.world"),
					},
					{Config: config, Destroy: true},
				},
			})

			body, deleted := recorded()
			if !deleted {
				t.Fatal("Expected the machine to be deleted")
			}
			if body != tc.wantBody {
				t.Errorf("Expected the delete body '%s', got '%s'", tc.wantBody, body)
			}
		})
	}
}

const vmPlanTestLateMac = "fa:16:3e:00:00:01"

// vmLateMacPlanTestInterfacesBody reports the birth interface with or without its MAC.
// GPCN leaves the column null until the port materializes.
func vmLateMacPlanTestInterfacesBody(withMac bool) map[string]any {
	body := vmSegmentPlanTestNetworkInterfacesBody(vmPlanTestSubnetID, nil)
	if !withMac {
		body["data"].([]map[string]any)[0]["macAddress"] = nil
	}
	return body
}

// startVirtualMachineLateMacMockServer materializes the MAC of the birth interface at
// the rename, which is after the plan and inside the apply. A test then drives the one
// window where the interface list moves under a plan that pinned it.
func startVirtualMachineLateMacMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	renamed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			withMac := renamed
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmLateMacPlanTestInterfacesBody(withMac))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			renamed = true
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// A rename changes no network input, so the plan pins network_interfaces to state.
// Terraform refuses a state that differs from that plan. The platform can fill a late
// column in the same apply. The update writes the pinned list, and the next refresh
// records the column.
func TestVirtualMachineResourcePlanRenameKeepsThePinnedInterfaceList(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server := startVirtualMachineLateMacMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-late-mac"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.mac_address"),
				),
			},
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-late-mac-renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-late-mac-renamed"),
				),
			},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.mac_address", vmPlanTestLateMac),
				),
			},
		},
	})
}

const (
	vmPlanTestBirthAddress = "203.0.113.30"
	vmPlanTestMovedAddress = "203.0.113.31"
)

// vmMovedAddressPlanTestInterfacesBody reports the birth interface with the address it
// carries now. A VPC interface reports the address row id beside the address.
func vmMovedAddressPlanTestInterfacesBody(address string) map[string]any {
	body := vmSegmentPlanTestNetworkInterfacesBody(vmPlanTestSubnetID, nil)
	row := body["data"].([]map[string]any)[0]
	row["publicIp"] = address
	row["publicIpId"] = vmPlanTestAcquiredIpID
	return body
}

// startVirtualMachineMovedAddressMockServer moves the address of the birth interface at
// the rename, which is after the plan and inside the apply. A test then drives the one
// window where the address moves under a plan that pinned it.
func startVirtualMachineMovedAddressMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	renamed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			address := vmPlanTestBirthAddress
			if renamed {
				address = vmPlanTestMovedAddress
			}
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmMovedAddressPlanTestInterfacesBody(address))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
		case r.Method == http.MethodPut && r.URL.Path == vmPlanTestPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			if updated, ok := body["name"].(string); ok {
				name = updated
			}
			renamed = true
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && r.URL.Path == vmPlanTestPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// A rename asks for no address, so the plan pins public_ip to state. Terraform refuses a
// state that differs from that plan. GPCN can move the address inside the same apply.
// The update writes the pinned address, and the next refresh records the new one.
func TestVirtualMachineResourcePlanRenameKeepsThePinnedPublicIp(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server := startVirtualMachineMovedAddressMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-moved-address"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestBirthAddress),
				),
			},
			{
				Config: vmPlanTestConfig(server.URL, "vm-plan-moved-address-renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "vm-plan-moved-address-renamed"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestBirthAddress),
				),
			},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "public_ip", vmPlanTestMovedAddress),
				),
			},
		},
	})
}

// The tail of an update writes state before it starts the machine. A start that fails
// therefore changes nothing the next apply repeats. The remedy sends the user to the
// portal alone, and the plan that follows is a no-op.
func TestVirtualMachineResourcePlanUpdateReportsFailedRestart(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, _ := startVirtualMachineSegmentHotplugMockServer(t, 0, false)

	config := vmSegmentListPlanTestConfig(server.URL, "vm-plan-restart-update", vmPlanTestSegmentID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmSegmentListPlanTestConfig(server.URL, "vm-plan-restart-update"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)Virtual\s+machine\s+left\s+stopped.*did\s+not\s+start\s+again.*Start\s+it\s+in\s+the\s+portal\.`),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// The create stops the machine to attach the segment. A start that fails leaves the
// machine stopped, and only an error tells the user so. The machine exists at the API,
// so it stays in state, and the error taints it. The remedy has to say so.
func TestVirtualMachineResourcePlanCreateReportsFailedRestart(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachineSegmentHotplugMockServer(t, 0, false)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vmSegmentListPlanTestConfig(server.URL, "vm-plan-restart-create", vmPlanTestSegmentID),
				ExpectError: regexp.MustCompile(`(?s)left\s+stopped.*did\s+not\s+start\s+again.*otherwise\s+the\s+next\s+apply\s+replaces\s+the\s+machine`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "l2_segment_ids.#", "1"),
				),
			},
		},
	})

	if starts := lastIndexOfRequest(recorded(), "POST "+vmPlanTestPath+"/start"); starts < 0 {
		t.Errorf("Expected a start, got %v", recorded())
	}
}
