package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/virtualmachines"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	vmPlanTestID           = "vm-1"
	vmPlanTestDatacenterID = "dc-1"
	vmPlanTestSizeID       = "sku-1"
	vmPlanTestSizeCode     = "G-Small-1"
	vmPlanTestImageID      = "img-1"
	vmPlanTestImageName    = "ubuntu-22.04"
	vmPlanTestNetworkID    = "net-1"
	vmPlanTestSshKeyID     = "key-1"
	vmPlanTestUsername     = "ubuntu"
	vmPlanTestTimestamp    = "2026-01-02T15:04:05Z"
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

// The birth network comes from the create body. The interface the API reports is
// therefore the one the provider asked for. A fixed fixture could agree by coincidence.
func vmPlanTestNetworkInterfacesBody(birthNetworkID string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": []map[string]any{{
			"id":               "nic-1",
			"networkInterface": 0,
			"isPrimary":        1,
			"publicIp":         "",
			"publicIpId":       "",
			"privateIp":        "10.0.0.5",
			"networkName":      "net-standard",
			"networkId":        birthNetworkID,
			"cidrBlock":        "10.0.0.0/24",
			"gatewayIp":        "10.0.0.1",
			"networkType":      "standard",
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
	birthNetworkID := ""
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
			birthNetworkID, _ = body["networkId"].(string)
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
			currentNetwork := birthNetworkID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentNetwork))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
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
  network_ids        = [%q]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestNetworkID, vmPlanTestSshKeyID, vmPlanTestUsername)
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

const (
	vmPlanTestSecondNetworkID = "net-2"
	vmPlanTestThirdNetworkID  = "net-3"
)

// vmAttachPlanTestNetworkInterfacesBody lists the birth interface and one row per
// network that attached. A test can then observe the state a failed attach leaves behind.
func vmAttachPlanTestNetworkInterfacesBody(birthNetworkID string, attached []string) map[string]any {
	rows := make([]map[string]any, 0, 1+len(attached))
	rows = append(rows, map[string]any{
		"id":               "nic-1",
		"networkInterface": 0,
		"isPrimary":        1,
		"publicIp":         "",
		"publicIpId":       "",
		"privateIp":        "10.0.0.5",
		"networkName":      "net-standard",
		"networkId":        birthNetworkID,
		"cidrBlock":        "10.0.0.0/24",
		"gatewayIp":        "10.0.0.1",
		"networkType":      "standard",
	})
	for index, networkID := range attached {
		rows = append(rows, map[string]any{
			"id":               fmt.Sprintf("nic-%d", index+2),
			"networkInterface": index + 1,
			"isPrimary":        0,
			"publicIp":         "",
			"publicIpId":       "",
			"privateIp":        "10.0.1.5",
			"networkName":      "net-extra",
			"networkId":        networkID,
			"cidrBlock":        "10.0.1.0/24",
			"gatewayIp":        "10.0.1.1",
			"networkType":      "standard",
		})
	}
	return map[string]any{"success": true, "message": "ok", "data": rows}
}

// startVirtualMachineAttachMockServer refuses the post-create attach until the returned
// function heals it. The refusal is a 500, and the provider configuration asks for no
// retries. The attach therefore fails on the first call.
func startVirtualMachineAttachMockServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthNetworkID := ""
	attachFails := true
	status := virtualmachines.VMStatusRunning.String()
	var attached []string

	vmPath := "/v1/resource/virtual-machines/" + vmPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/virtual-machines/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			birthNetworkID, _ = body["networkId"].(string)
			attached = nil
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			refuse := attachFails
			if !refuse {
				networkID, _ := body["networkId"].(string)
				attached = append(attached, networkID)
			}
			mu.Unlock()
			if refuse {
				w.WriteHeader(http.StatusInternalServerError)
				testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "attach refused"})
				return
			}
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentBirth := birthNetworkID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmAttachPlanTestNetworkInterfacesBody(currentBirth, current))
		case r.Method == http.MethodPost && r.URL.Path == vmPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBody(currentName, currentStatus, vmPlanTestSizeID))
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

	heal := func() {
		mu.Lock()
		attachFails = false
		mu.Unlock()
	}

	return server, heal
}

func vmAttachPlanTestConfig(host, name string) string {
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
  network_ids        = [%q, %q]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, vmPlanTestNetworkID, vmPlanTestSecondNetworkID, vmPlanTestSshKeyID, vmPlanTestUsername)
}

// A failed attach must not orphan the machine: it exists at the API, so it belongs in
// state. State must also name only the networks that attached, or no later plan can
// attach the rest. Terraform taints a resource whose create returned an error, so the
// last step plans a replacement. A machine absent from state would plan a bare create
// instead, with nothing to destroy.
func TestVirtualMachineResourcePlanCreateWritesStateWhenAttachFails(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, heal := startVirtualMachineAttachMockServer(t)

	config := vmAttachPlanTestConfig(server.URL, "vm-plan-attach")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)attaching .* failed.*terraform\s+untaint`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_ids.#", "1"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_ids.0", vmPlanTestNetworkID),
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
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "id"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.#", "2"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.0.network_id", vmPlanTestNetworkID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_interfaces.1.network_id", vmPlanTestSecondNetworkID),
				),
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
	birthNetworkID := ""
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
			birthNetworkID, _ = body["networkId"].(string)
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodGet && r.URL.Path == vmPath+"/network-interfaces":
			mu.Lock()
			currentNetwork := birthNetworkID
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestNetworkInterfacesBody(currentNetwork))
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

const vmPlanTestPath = "/v1/resource/virtual-machines/" + vmPlanTestID

func vmPlanTestReadBodyWithHotplug(name, status string, hotplug int) map[string]any {
	body := vmPlanTestReadBody(name, status, vmPlanTestSizeID)
	body["data"].(map[string]any)["networkHotplug"] = hotplug
	return body
}

// startVirtualMachineHotplugMockServer accepts every call and records the order in which
// they arrive. A test reads the order to learn whether the provider stopped the machine
// around the post-create attach.
func startVirtualMachineHotplugMockServer(t *testing.T, hotplug int) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthNetworkID := ""
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
			birthNetworkID, _ = body["networkId"].(string)
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			networkID, _ := body["networkId"].(string)
			mu.Lock()
			attached = append(attached, networkID)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentBirth := birthNetworkID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmAttachPlanTestNetworkInterfacesBody(currentBirth, current))
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
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
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, hotplug))
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

func TestVirtualMachineResourcePlanCreateStopsForAttachWithoutHotplug(t *testing.T) {
	tests := []struct {
		name          string
		hotplug       int
		networkIDs    []string
		expectStopped bool
	}{
		// Three networks mean two attaches. A start after the first attach then differs
		// from a start after the last one.
		{"without hotplug the machine stops around the attach", 0, []string{vmPlanTestNetworkID, vmPlanTestSecondNetworkID, vmPlanTestThirdNetworkID}, true},
		{"with hotplug the machine stays running", 1, []string{vmPlanTestNetworkID, vmPlanTestSecondNetworkID}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortenVirtualMachinePolling(t)
			server, recorded := startVirtualMachineHotplugMockServer(t, tc.hotplug)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: vmNetworkListPlanTestConfig(server.URL, "vm-plan-hotplug", tc.networkIDs...),
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

// startVirtualMachineFailedRestartMockServer refuses every start. The machine it reports
// has no network hotplug, so the provider stops it before it changes the networks. A test
// then observes what the provider does with a machine it cannot start again.
func startVirtualMachineFailedRestartMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	name := ""
	birthNetworkID := ""
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
			birthNetworkID, _ = body["networkId"].(string)
			attached = nil
			status = virtualmachines.VMStatusRunning.String()
			mu.Unlock()
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", vmPlanTestID, true)
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			body := testutil.ReadRequestBody(r)
			networkID, _ := body["networkId"].(string)
			mu.Lock()
			attached = append(attached, networkID)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-3", "attach issued")
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath+"/network-interfaces":
			mu.Lock()
			currentBirth := birthNetworkID
			current := append([]string(nil), attached...)
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmAttachPlanTestNetworkInterfacesBody(currentBirth, current))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, vmPlanTestPath+"/network-interfaces/"):
			mu.Lock()
			attached = attached[:0]
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-4", "detach issued")
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/stop":
			mu.Lock()
			status = virtualmachines.VMStatusShutoff.String()
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodPost && r.URL.Path == vmPlanTestPath+"/start":
			w.WriteHeader(http.StatusInternalServerError)
			testutil.WriteJSONResponse(w, map[string]any{"success": false, "message": "start refused"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/resource/data-centers/"+vmPlanTestDatacenterID+"/virtual-machine-sizes":
			testutil.WriteJSONResponse(w, vmPlanTestSizesBody())
		case r.Method == http.MethodGet && r.URL.Path == vmPlanTestPath:
			mu.Lock()
			currentName, currentStatus := name, status
			mu.Unlock()
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, 0))
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

	return server
}

// vmNetworkListPlanTestConfig takes any number of networks, so a test can observe the
// attach loop. It asks for no retries, so a refused call fails on the first attempt.
func vmNetworkListPlanTestConfig(host, name string, networkIDs ...string) string {
	quoted := make([]string, 0, len(networkIDs))
	for _, networkID := range networkIDs {
		quoted = append(quoted, fmt.Sprintf("%q", networkID))
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
  network_ids        = [%s]
  initial_auth = {
    ssh_key_id = %q
    username   = %q
  }
}
`, host, name, vmPlanTestDatacenterID, vmPlanTestSizeID, vmPlanTestImageID, strings.Join(quoted, ", "), vmPlanTestSshKeyID, vmPlanTestUsername)
}

// The create stops the machine to attach the second network. A start that fails leaves
// the machine stopped, and only an error tells the user so. The machine exists at the
// API, so it stays in state.
func TestVirtualMachineResourcePlanCreateReportsFailedRestart(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server := startVirtualMachineFailedRestartMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vmNetworkListPlanTestConfig(server.URL, "vm-plan-restart-create", vmPlanTestNetworkID, vmPlanTestSecondNetworkID),
				ExpectError: regexp.MustCompile(`(?s)left\s+stopped.*did not start again`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_ids.#", "2"),
				),
			},
		},
	})
}

// The update stops the machine to attach the added network. Only an error tells the user
// the machine stayed stopped.
func TestVirtualMachineResourcePlanUpdateReportsFailedRestart(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server := startVirtualMachineFailedRestartMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vmNetworkListPlanTestConfig(server.URL, "vm-plan-restart-update", vmPlanTestNetworkID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
				),
			},
			{
				Config:      vmNetworkListPlanTestConfig(server.URL, "vm-plan-restart-update", vmPlanTestNetworkID, vmPlanTestSecondNetworkID),
				ExpectError: regexp.MustCompile(`(?s)left\s+stopped.*did not start again`),
			},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "id", vmPlanTestID),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "network_ids.#", "2"),
				),
			},
		},
	})
}
