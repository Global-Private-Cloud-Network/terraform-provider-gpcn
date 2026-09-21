package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// startNetworkPlanMockServer serves the network endpoints a rename needs, starting from a
// network that already exists: creation is retired, so a plan test reaches an existing row
// through terraform import. The handler keeps the name from the last update, so the read
// after an apply matches the configuration and the refresh plan stays empty. The returned
// function renames the network out of band, which is how a test creates drift. There is no
// create route, so a plan that tried to mint a network fails the test loudly.
func startNetworkPlanMockServer(t *testing.T, initialName string) (*httptest.Server, func(string)) {
	t.Helper()

	var mu sync.Mutex
	name := initialName

	networkPath := "/v1/resource/networks/" + networkPlanTestID
	virtualMachinesPath := networkPath + "/virtual-machines"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
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

// TestNetworkResourcePlanRefusesCreate guards the create refusal. The platform retired the
// legacy create verb, so a plan that would mint a network must fail before any request goes out.
func TestNetworkResourcePlanRefusesCreate(t *testing.T) {
	t.Parallel()
	server, _ := startNetworkPlanMockServer(t, "net-plan-new")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      networkPlanTestConfig(server.URL, "net-plan-new"),
				ExpectError: regexp.MustCompile("Network creation is no longer supported"),
			},
		},
	})
}

// TestNetworkResourcePlanRename pins the in-place rename path of a grandfathered network.
// The first step imports, because the create refusal closes the path this test used to take.
func TestNetworkResourcePlanRename(t *testing.T) {
	t.Parallel()
	server, _ := startNetworkPlanMockServer(t, "net-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             networkPlanTestConfig(server.URL, "net-plan-a"),
				ResourceName:       gpcnNetworkTest,
				ImportState:        true,
				ImportStateId:      networkPlanTestID,
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported state, got %d", len(states))
					}
					for attribute, want := range map[string]string{
						"name":       "net-plan-a",
						"gateway_ip": networkPlanTestGatewayIP,
						"cidr_block": networkPlanTestCIDRBlock,
					} {
						if got := states[0].Attributes[attribute]; got != want {
							return fmt.Errorf("imported %s = %q, want %q", attribute, got, want)
						}
					}
					return nil
				},
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
					resource.TestCheckResourceAttr(gpcnNetworkTest, "gateway_ip", networkPlanTestGatewayIP),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "cidr_block", networkPlanTestCIDRBlock),
				),
			},
		},
	})
}

func TestNetworkResourcePlanDetectsOutOfBandRename(t *testing.T) {
	t.Parallel()
	server, setName := startNetworkPlanMockServer(t, "net-plan-a")

	config := networkPlanTestConfig(server.URL, "net-plan-a")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       gpcnNetworkTest,
				ImportState:        true,
				ImportStateId:      networkPlanTestID,
				ImportStatePersist: true,
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

const networkPlanTestCustomID = "net-custom-1"

// customNetworkPlanMock serves one grandfathered custom network. The API stores an empty
// cidrBlock for every custom row, so the mock returns one and records the update body the
// provider sends back.
type customNetworkPlanMock struct {
	mu          sync.Mutex
	description string
	lastPutBody map[string]any
	gone        bool
}

// setGone makes the network answer the 404 that the platform serves for a row it adopted
// into an L2 segment. The body is the one the backend renders, byte for byte.
func (m *customNetworkPlanMock) setGone() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gone = true
}

func (m *customNetworkPlanMock) isGone() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gone
}

func (m *customNetworkPlanMock) readBody() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"success": true,
		"message": "ok",
		"data": map[string]any{
			"id":              networkPlanTestCustomID,
			"name":            "net-custom-a",
			"description":     m.description,
			"createdAt":       networkPlanTestTimestamp,
			"updatedAt":       networkPlanTestTimestamp,
			"snat":            "false",
			"cidrBlock":       "",
			"gatewayIp":       "",
			"connectedVms":    "0",
			"networkType":     "custom",
			"dnsNameservers":  "",
			"allocationPools": []map[string]any{},
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

func (m *customNetworkPlanMock) updateBody() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPutBody
}

func startCustomNetworkPlanMockServer(t *testing.T) (*httptest.Server, *customNetworkPlanMock) {
	t.Helper()

	mock := &customNetworkPlanMock{description: "before"}
	networkPath := "/v1/resource/networks/" + networkPlanTestCustomID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", networkPlanTestCustomID, true)
		case r.Method == http.MethodPut && r.URL.Path == networkPath:
			body := testutil.ReadRequestBody(r)
			mock.mu.Lock()
			mock.lastPutBody = body
			mock.description, _ = body["description"].(string)
			mock.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true})
		case r.Method == http.MethodGet && r.URL.Path == networkPath+"/virtual-machines":
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "ok", "data": []map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == networkPath:
			if mock.isGone() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"Network not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
				return
			}
			testutil.WriteJSONResponse(w, mock.readBody())
		case r.Method == http.MethodDelete && r.URL.Path == networkPath:
			testutil.HandleCreateJobResponse(w, "job-2", "delete issued")
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, mock
}

func customNetworkPlanTestConfig(host, description string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_network" "test" {
  name          = "net-custom-a"
  datacenter_id = %q
  network_type  = "custom"
  description   = %q
}
`, host, networkPlanTestDatacenterID, description)
}

// TestNetworkResourcePlanUpdatesExistingCustomWithoutCidrBlock guards the whole path a
// grandfathered custom network takes through a rename: its stored cidrBlock is "", and the
// update body must not carry that value back, or the API answers 422.
func TestNetworkResourcePlanUpdatesExistingCustomWithoutCidrBlock(t *testing.T) {
	t.Parallel()
	server, mock := startCustomNetworkPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             customNetworkPlanTestConfig(server.URL, "before"),
				ResourceName:       gpcnNetworkTest,
				ImportState:        true,
				ImportStateId:      networkPlanTestCustomID,
				ImportStatePersist: true,
			},
			{
				Config: customNetworkPlanTestConfig(server.URL, "after"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnNetworkTest, "description", "after"),
					func(*terraform.State) error {
						body := mock.updateBody()
						if body == nil {
							return fmt.Errorf("expected the update endpoint to be called")
						}
						if value, present := body["cidrBlock"]; present {
							return fmt.Errorf("expected cidrBlock to be absent from the update body, got %q", value)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestNetworkResourcePlanWarnsWhenCustomNetworkGone pins what a refresh does with a custom
// network the platform adopted: the row leaves state, so the next plan proposes a create and
// the create refusal answers. The warning the refresh also emits is pinned by
// TestCustomNetworkGoneWarningUnit, because this harness cannot observe a warning diagnostic.
func TestNetworkResourcePlanWarnsWhenCustomNetworkGone(t *testing.T) {
	t.Parallel()
	server, mock := startCustomNetworkPlanMockServer(t)

	config := customNetworkPlanTestConfig(server.URL, "before")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       gpcnNetworkTest,
				ImportState:        true,
				ImportStateId:      networkPlanTestCustomID,
				ImportStatePersist: true,
			},
			{
				PreConfig:   func() { mock.setGone() },
				Config:      config,
				ExpectError: regexp.MustCompile("Network creation is no longer supported"),
			},
		},
	})
}
