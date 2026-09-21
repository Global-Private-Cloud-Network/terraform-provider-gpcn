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

	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/virtualmachines"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
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

func vmPlanTestReadBodyWithHotplug(name, status string, hotplug int) map[string]any {
	body := vmPlanTestReadBody(name, status, vmPlanTestSizeID)
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
// arrive. A test then reads what the provider changed and what it left alone.
func startVirtualMachineSegmentUpdateMockServer(t *testing.T, hotplug int) (*httptest.Server, func() []string) {
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
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, hotplug))
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

// A segment list changes in place. The provider detaches the interface that carries the
// segment the configuration dropped and attaches one for the segment it gained. An image
// with network hotplug takes the change while the machine runs.
func TestVirtualMachineResourcePlanAttachesAndDetachesSegments(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachineSegmentUpdateMockServer(t, 1)

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
// nothing. A machine without network hotplug stops for a segment change, and stopping it
// for a list that carries the same segments costs the user the whole downtime.
func TestVirtualMachineResourcePlanReorderedSegmentsChangeNothing(t *testing.T) {
	shortenVirtualMachinePolling(t)
	server, recorded := startVirtualMachineSegmentUpdateMockServer(t, 0)

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
			testutil.WriteJSONResponse(w, vmPlanTestReadBodyWithHotplug(currentName, currentStatus, hotplug))
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
				ExpectError: regexp.MustCompile(`(?s)Error\s+updating\s+network\s+interfaces.*Virtual\s+machine\s+left\s+stopped.*did\s+not\s+start\s+again.*the\s+change\s+was\s+not\s+recorded,\s+so\s+the\s+next\s+apply\s+retries\s+it\.`),
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
				ExpectError: regexp.MustCompile(`(?s)Retrieving\s+information\s+about\s+the\s+Virtual\s+Machine\s+failed`),
			},
		},
	})

	if starts := startCount(); starts != 1 {
		t.Errorf("Expected exactly 1 start, got %d", starts)
	}
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
// order. The legacy per-NIC routes answer nothing but a test failure: GPCN refuses them
// on a VPC interface, so the provider must never reach for one. A create that names an
// address binds it, and the image list answers the import.
func startVirtualMachinePublicIpMockServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	name := ""
	status := virtualmachines.VMStatusRunning.String()
	boundID := ""
	boundAddress := ""
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
	server, recorded := startVirtualMachinePublicIpMockServer(t)

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
	server, recorded := startVirtualMachinePublicIpMockServer(t)

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
	server, _ := startVirtualMachinePublicIpMockServer(t)

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

// startVirtualMachineDestroyBodyMockServer keeps whichever address the create asked for
// and records the body of the delete. A test then reads what the destroy asked GPCN to
// do with that address.
func startVirtualMachineDestroyBodyMockServer(t *testing.T) (*httptest.Server, func() (string, bool)) {
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
// GPCN keeps it.
func TestVirtualMachineResourcePlanDestroyReleasesAcquiredIp(t *testing.T) {
	tests := []struct {
		name       string
		allocate   bool
		publicIpID string
		wantBody   string
	}{
		{"an acquired address is released", true, "", `{"releasePublicIps":true}`},
		{"a held address is kept", false, vmPlanTestHeldIpID, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shortenVirtualMachinePolling(t)
			server, recorded := startVirtualMachineDestroyBodyMockServer(t)

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

// The create stops the machine to attach the segment. A start that fails leaves the
// machine stopped, and only an error tells the user so. The machine exists at the API,
// so it stays in state, and the error taints it: the remedy has to say so.
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
