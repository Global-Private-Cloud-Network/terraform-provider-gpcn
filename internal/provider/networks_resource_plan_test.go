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
	networkPlanTestID           = "net-1"
	networkPlanTestDatacenterID = "dc-1"
	networkPlanTestCIDRBlock    = "10.10.0.0/24"
	networkPlanTestGatewayIP    = "10.10.0.1"
	networkPlanTestDHCPStart    = "10.10.0.10"
	networkPlanTestDHCPEnd      = "10.10.0.100"
	networkPlanTestDNSServer    = "8.8.8.8"
	networkPlanTestTimestamp    = "2026-01-02T15:04:05Z"
)

func networkPlanTestReadBody(name string) map[string]any {
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"id":             networkPlanTestID,
			"name":           name,
			"description":    "",
			"createdAt":      networkPlanTestTimestamp,
			"updatedAt":      networkPlanTestTimestamp,
			"snat":           "true",
			"cidrBlock":      networkPlanTestCIDRBlock,
			"gatewayIp":      networkPlanTestGatewayIP,
			"connectedVms":   "0",
			"networkType":    "standard",
			"dnsNameservers": networkPlanTestDNSServer,
			"allocationPools": []map[string]any{{
				"start": networkPlanTestDHCPStart,
				"end":   networkPlanTestDHCPEnd,
			}},
			"datacenter": map[string]any{
				"id":          networkPlanTestDatacenterID,
				"name":        "Kansas",
				"region":      "central",
				"countryAbbr": "US",
				"country":     "United States",
			},
		},
	}
}

// startNetworkPlanMockServer serves the network endpoints a rename needs. The handler
// keeps the name from the last create or update. The read after an apply then matches the
// configuration, so the refresh plan stays empty. The returned function renames the
// network out of band, which is how a test creates drift.
func startNetworkPlanMockServer(t *testing.T) (*httptest.Server, func(string)) {
	t.Helper()

	var mu sync.Mutex
	name := ""

	networkPath := "/v1/resource/networks/" + networkPlanTestID
	virtualMachinesPath := networkPath + "/virtual-machines"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/networks/":
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-1", "create issued")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", networkPlanTestID, true)
		case r.Method == http.MethodPut && r.URL.Path == networkPath:
			body := testutil.ReadRequestBody(r)
			mu.Lock()
			name, _ = body["name"].(string)
			mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == virtualMachinesPath:
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "ok", "data": []map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == networkPath:
			mu.Lock()
			current := name
			mu.Unlock()
			testutil.WriteJSONResponse(w, networkPlanTestReadBody(current))
		case r.Method == http.MethodDelete && r.URL.Path == networkPath:
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

func networkPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_network" "test" {
  name               = %q
  datacenter_id      = %q
  network_type       = "standard"
  cidr_block         = %q
  dhcp_start_address = %q
  dhcp_end_address   = %q
  dns_servers        = [%q]
}
`, host, name, networkPlanTestDatacenterID, networkPlanTestCIDRBlock, networkPlanTestDHCPStart, networkPlanTestDHCPEnd, networkPlanTestDNSServer)
}

// TestNetworkResourcePlanRename pins the in-place rename path; it does not guard a fix.
func TestNetworkResourcePlanRename(t *testing.T) {
	t.Parallel()
	server, _ := startNetworkPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: networkPlanTestConfig(server.URL, "net-plan-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "net-plan-a"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "gateway_ip", networkPlanTestGatewayIP),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "cidr_block", networkPlanTestCIDRBlock),
				),
			},
			{
				Config: networkPlanTestConfig(server.URL, "net-plan-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "net-plan-b"),
				),
			},
		},
	})
}

func TestNetworkResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	server, setName := startNetworkPlanMockServer(t)

	config := networkPlanTestConfig(server.URL, "net-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionCreate),
					},
				},
			},
			{
				PreConfig: func() { setName("renamed-out-of-band") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "net-plan-a"),
				),
			},
		},
	})
}
