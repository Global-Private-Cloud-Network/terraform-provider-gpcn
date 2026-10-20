package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/vpcvpnconnections"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	vpnConnPlanTestVpcID       = "11111111-1111-4111-8111-111111111111"
	vpnConnPlanTestGatewayID   = "22222222-2222-4222-8222-222222222222"
	vpnConnPlanTestConnID      = "33333333-3333-4333-8333-333333333333"
	vpnConnPlanTestOtherGWID   = "44444444-4444-4444-8444-444444444444"
	vpnConnPlanTestTimestamp   = "2026-01-02T15:04:05Z"
	vpnConnPlanTestPsk         = "Tr0ub4dor&3-horse-staple"
	vpnConnPlanTestPskTwo      = "Zx9!correct-battery-wire"
	vpnConnPlanTestLocalEnd    = "198.51.100.7"
	vpnConnPlanTestConflict    = "A change to this VPN connection is already in progress; wait for it to finish."
	vpnConnPlanTestFailReason  = "VPN connection creation failed; delete it and create it again"
	vpnConnPlanTestParkedText  = "Applying the VPN connection change failed; your saved settings are kept — update to retry"
	vpnConnPlanTestJobFailText = "Applying the VPN connection change failed; your saved settings are kept - update to retry"
	vpnConnPlanTestBadGateway  = "The upstream VPN service answered badly."
	vpnConnPlanTestGetRefused  = "You may not read this VPN connection."
	gpcnVpcVpnConnectionTest   = "gpcn_vpc_vpn_connection.test"
)

var vpnConnPlanTestLocalNetworks = []string{"10.121.1.0/24", "10.121.2.0/24"}

// vpnConnPlanTestServerState is the VPN connection the mock keeps between requests.
type vpnConnPlanTestServerState struct {
	mu            sync.Mutex
	gatewayExists bool
	exists        bool
	row           map[string]any
	// createdState is the state the connection reads in once its job ends.
	createdState string
	// failJob makes the job poll report a failed job.
	failJob bool
	// deleteConflict answers the next DELETE with a 409 carrying this message.
	deleteConflict string
	desiredApplied bool
	// putSettles makes a PUT settle the row, as the job of an update does: the
	// state becomes connecting and the failure reason clears.
	putSettles bool
	// postStatus makes the POST insert the row and then answer with this status.
	postStatus int
	// itemStatus makes the detail GET answer with this status.
	itemStatus int
	// keepRow makes the POST leave the stored row as the test planted it.
	keepRow bool
	// postActiveJob is the job the row made by the POST still runs.
	postActiveJob string
	// listStatus makes the list GET answer with this status.
	listStatus int
	// deleteRemoving answers the next DELETE with a 409 for a connection that
	// is already being removed.
	deleteRemoving bool
	// removingHold is the number of detail reads after that 409 that still show
	// the row as removing. The read after that answers 404.
	removingHold int
	removing     bool
	getsAfterDel int
	postBodies   []map[string]any
	postRaw      []string
	putBodies    []map[string]any
	deleteCount  int
}

func (s *vpnConnPlanTestServerState) set(update func(*vpnConnPlanTestServerState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(s)
}

func (s *vpnConnPlanTestServerState) putKeys() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([][]string, 0, len(s.putBodies))
	for _, body := range s.putBodies {
		keys := make([]string, 0, len(body))
		for key := range body {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		all = append(all, keys)
	}
	return all
}

func (s *vpnConnPlanTestServerState) detail() map[string]any {
	out := map[string]any{}
	for key, value := range s.row {
		out[key] = value
	}
	out["parameterSheet"] = map[string]any{"localNetworks": vpnConnPlanTestLocalNetworks}
	return out
}

func vpnConnPlanTestRowFromBody(body map[string]any, state string) map[string]any {
	row := map[string]any{
		"id": vpnConnPlanTestConnID, "name": body["name"], "description": nil, "state": state,
		"providerState": nil, "failureReason": nil, "gatewayKind": "router", "localEndpointAddress": vpnConnPlanTestLocalEnd,
		"peerAddress": body["peerAddress"], "peerCidrs": body["peerCidrs"],
		"ikeVersion": "v2", "encryption": "aes256", "integrity": "sha256", "dhGroup": 14, "pfsGroup": 14,
		"ikeLifetimeSeconds": 28800, "ipsecLifetimeSeconds": 3600, "dpd": true,
		"desiredApplied": true, "activeJobId": nil, "acceptedVerbs": []string{"update", "reset", "delete"},
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano), "updatedAt": vpnConnPlanTestTimestamp,
	}
	for _, key := range []string{"description", "ikeVersion", "encryption", "integrity", "dhGroup", "pfsGroup", "ikeLifetimeSeconds", "ipsecLifetimeSeconds", "dpd"} {
		if value, ok := body[key]; ok {
			row[key] = value
		}
	}
	if state == "failed" {
		row["failureReason"] = vpnConnPlanTestFailReason
	}
	return row
}

func writeVpnConnPlanTestError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"success":false,"message":%q,"error":{"code":%q,"statusCode":%d,"details":null}}`, message, code, status)
}

func writeVpnConnPlanTestRemoving(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	fmt.Fprint(w, `{"success":false,"message":"The VPN connection is being removed.","error":{"code":"VPN_STATE_CONFLICT","statusCode":409,`+
		`"details":{"state":"removing","verb":"delete","failureReason":null}}}`)
}

func init() {
	vpcvpnconnections.RemovalPollInterval = 5 * time.Millisecond
}

func startVpnConnPlanMockServer(t *testing.T) (*httptest.Server, *vpnConnPlanTestServerState) {
	t.Helper()

	state := &vpnConnPlanTestServerState{gatewayExists: true, createdState: "connecting", desiredApplied: true}
	collection := "/v1/resource/vpcs/" + vpnConnPlanTestVpcID + "/vpn-connections"
	item := collection + "/" + vpnConnPlanTestConnID
	gatewayPath := "/v1/resource/vpcs/" + vpnConnPlanTestVpcID + "/vpn-gateway"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodGet && r.URL.Path == gatewayPath:
			state.mu.Lock()
			exists := state.gatewayExists
			state.mu.Unlock()
			if !exists {
				writeVpnConnPlanTestError(w, http.StatusNotFound, "VPN_GATEWAY_NOT_FOUND", "This VPC has no VPN gateway.")
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "meta": nil, "data": map[string]any{
				"id": vpnConnPlanTestGatewayID, "vpcId": vpnConnPlanTestVpcID, "kind": "router", "state": "ready",
				"address": vpnConnPlanTestLocalEnd, "reservedBlock": nil, "failureReason": nil, "activeJobId": nil,
				"createdAt": vpnConnPlanTestTimestamp, "updatedAt": vpnConnPlanTestTimestamp,
			}})
		case r.Method == http.MethodPost && r.URL.Path == collection:
			raw := new(bytes.Buffer)
			_, _ = raw.ReadFrom(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw.Bytes(), &body)
			state.mu.Lock()
			state.postBodies = append(state.postBodies, body)
			state.postRaw = append(state.postRaw, raw.String())
			if !state.keepRow {
				state.exists = true
				state.row = vpnConnPlanTestRowFromBody(body, state.createdState)
				if state.postActiveJob != "" {
					state.row["activeJobId"] = state.postActiveJob
				}
				state.desiredApplied = true
			}
			postStatus := state.postStatus
			state.mu.Unlock()
			if postStatus != 0 {
				writeVpnConnPlanTestError(w, postStatus, "Internal Server Error", vpnConnPlanTestBadGateway)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true, "message": "Operation initiated successfully", "meta": nil,
				"data": map[string]any{"jobId": "job-create", "vpnConnection": map[string]any{"id": vpnConnPlanTestConnID, "state": "creating"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == collection:
			state.mu.Lock()
			rows := []map[string]any{}
			if state.exists {
				rows = append(rows, state.detail())
			}
			listStatus := state.listStatus
			state.mu.Unlock()
			if listStatus != 0 {
				writeVpnConnPlanTestError(w, listStatus, "Insufficient Permissions", vpnConnPlanTestGetRefused)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": rows, "meta": map[string]any{"hasNextPage": false}})
		case r.Method == http.MethodGet && r.URL.Path == item:
			state.mu.Lock()
			if state.removing {
				state.getsAfterDel++
				if state.removingHold > 0 {
					state.removingHold--
					state.row["state"] = "removing"
				} else {
					state.exists = false
				}
			}
			exists := state.exists
			itemStatus := state.itemStatus
			detail := state.detail()
			detail["desiredApplied"] = state.desiredApplied
			state.mu.Unlock()
			if itemStatus != 0 {
				writeVpnConnPlanTestError(w, itemStatus, "Insufficient Permissions", vpnConnPlanTestGetRefused)
				return
			}
			if !exists {
				writeVpnConnPlanTestError(w, http.StatusNotFound, "Resource Not Found", "VPN connection not found")
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": detail, "meta": nil})
		case r.Method == http.MethodPut && r.URL.Path == item:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.putBodies = append(state.putBodies, body)
			state.desiredApplied = true
			for key, value := range body {
				if key != "psk" {
					state.row[key] = value
				}
			}
			if state.putSettles {
				state.row["state"] = "connecting"
				state.row["failureReason"] = nil
			}
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true, "message": "Operation initiated successfully", "meta": nil,
				"data": map[string]any{"jobId": "job-update"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == item:
			state.mu.Lock()
			conflict := state.deleteConflict
			state.deleteConflict = ""
			alreadyRemoving := state.deleteRemoving
			state.deleteRemoving = false
			if alreadyRemoving {
				state.removing = true
			} else if conflict == "" {
				state.exists = false
				state.deleteCount++
			}
			state.mu.Unlock()
			if alreadyRemoving {
				writeVpnConnPlanTestRemoving(w)
				return
			}
			if conflict != "" {
				writeVpnConnPlanTestError(w, http.StatusConflict, "VPN_STATE_CONFLICT", conflict)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true, "message": "Operation initiated successfully", "meta": nil,
				"data": map[string]any{"jobId": "job-delete"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			jobID := "job-1"
			if ids, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(ids) > 0 {
				jobID, _ = ids[0].(string)
			}
			state.mu.Lock()
			failJob := state.failJob
			if active, _ := state.row["activeJobId"].(string); active != "" && jobID == active {
				state.row["activeJobId"] = nil
				state.row["state"] = "connecting"
			}
			state.mu.Unlock()
			if failJob && jobID != "job-delete" {
				testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{
					Success: true,
					Data: client.JobStatusDataResponse{Jobs: []client.JobResponse{{
						JobID: jobID, ResourceId: vpnConnPlanTestConnID, HasFailed: true, ErrorMessage: vpnConnPlanTestJobFailText,
					}}},
				})
				return
			}
			testutil.HandleJobResponse(w, jobID, vpnConnPlanTestConnID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

// vpnConnPlanTestConfig is the configuration the tests change one field at a time.
type vpnConnPlanTestConfig struct {
	GatewayID  string
	Name       string
	Desc       string
	Peer       string
	Cidrs      string
	PskLine    string
	Ike        string
	Encryption string
	Integrity  string
	Dh         int
	Pfs        int
	IkeLife    int
	IpsecLife  int
	Dpd        bool
}

func newVpnConnPlanTestConfig() vpnConnPlanTestConfig {
	return vpnConnPlanTestConfig{
		GatewayID: vpnConnPlanTestGatewayID, Name: "branch-office", Peer: "203.0.113.10",
		Cidrs:   `["192.168.10.0/24"]`,
		PskLine: fmt.Sprintf("psk_wo = %q\n  psk_wo_version = 1", vpnConnPlanTestPsk),
		Ike:     "v2", Encryption: "aes256", Integrity: "sha256", Dh: 14, Pfs: 14, IkeLife: 28800, IpsecLife: 3600, Dpd: true,
	}
}

func (c vpnConnPlanTestConfig) render(host string) string {
	description := ""
	if c.Desc != "" {
		description = fmt.Sprintf("description = %q", c.Desc)
	}
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_vpn_connection" "test" {
  vpc_id                 = %q
  vpn_gateway_id         = %q
  name                   = %q
  %s
  peer_address           = %q
  peer_cidrs             = %s
  %s
  ike_version            = %q
  encryption             = %q
  integrity              = %q
  dh_group               = %d
  pfs_group              = %d
  ike_lifetime_seconds   = %d
  ipsec_lifetime_seconds = %d
  dpd                    = %t
}
`, host, vpnConnPlanTestVpcID, c.GatewayID, c.Name, description, c.Peer, c.Cidrs, c.PskLine,
		c.Ike, c.Encryption, c.Integrity, c.Dh, c.Pfs, c.IkeLife, c.IpsecLife, c.Dpd)
}

// vpnConnRefusal builds the pattern for a message Terraform prints inside a
// wrapped diagnostic. It lets any run of whitespace stand for a space.
func vpnConnRefusal(message string) *regexp.Regexp {
	return regexp.MustCompile(strings.ReplaceAll(regexp.QuoteMeta(message), " ", `\s+`))
}

// vpnConnDetailOrder builds the pattern for a detail that starts with first and
// carries then after it. The prefix of each wrapped line may hold the box
// character that Terraform draws.
func vpnConnDetailOrder(first, then string) *regexp.Regexp {
	words := func(text string) string { return strings.ReplaceAll(regexp.QuoteMeta(text), " ", `[\s│]+`) }
	return regexp.MustCompile(`(?s)Unable to create GPCN™ VPN connection.*resource "gpcn_vpc_vpn_connection" "test" \{[\s│]+` + words(first) + `.*` + words(then))
}

func TestVpcVpnConnectionResourceCreate(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	checkPost := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if len(state.postBodies) != 1 {
			return fmt.Errorf("expected one create POST, got %d", len(state.postBodies))
		}
		body := state.postBodies[0]
		if body["psk"] != vpnConnPlanTestPsk {
			return fmt.Errorf("expected the POST to carry psk_wo as psk, got %v", body["psk"])
		}
		if _, found := body["description"]; found {
			return fmt.Errorf("expected no description key for an empty description, got %v", body["description"])
		}
		for _, key := range []string{"name", "peerAddress", "peerCidrs", "ikeVersion", "encryption", "integrity", "dhGroup", "pfsGroup", "ikeLifetimeSeconds", "ipsecLifetimeSeconds", "dpd"} {
			if _, found := body[key]; !found {
				return fmt.Errorf("expected the POST to carry %s", key)
			}
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "id", vpnConnPlanTestConnID),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "vpn_gateway_id", vpnConnPlanTestGatewayID),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "state", "connecting"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "description", ""),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "desired_applied", "true"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "gateway_kind", "router"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "local_endpoint_address", vpnConnPlanTestLocalEnd),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "local_networks.#", "2"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "local_networks.0", "10.121.1.0/24"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnConnectionTest, "failure_reason"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnConnectionTest, "psk_wo"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "psk_wo_version", "1"),
					resource.TestCheckTypeSetElemAttr(gpcnVpcVpnConnectionTest, "peer_cidrs.*", "192.168.10.0/24"),
					checkPost,
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// A connection that reads back failed after its job ended stops the apply with
// the failure reason. It stays in state tainted, so the next plan replaces it.
func TestVpcVpnConnectionResourceCreateFailed(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) { s.createdState = "failed" })
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnDetailOrder(vpnConnPlanTestFailReason, "")},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) {
						s.createdState = "connecting"
						s.row["state"] = "connecting"
						s.row["failureReason"] = nil
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "state", "connecting"),
			},
		},
	})
}

// A create job that stops leaves the row behind. The create writes it to state
// with the error, so Terraform taints it.
func TestVpcVpnConnectionResourceCreateJobFailedKeepsRow(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) { s.failJob = true; s.createdState = "failed" })
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnDetailOrder(vpnConnPlanTestFailReason, vpnConnPlanTestJobFailText)},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) {
						s.failJob = false
						s.createdState = "connecting"
						s.row["state"] = "connecting"
						s.row["failureReason"] = nil
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// A 5xx answer to the POST can hide a row the API did create. The POST goes out
// once, the create finds the row by name, and it saves the row with the error.
func TestVpcVpnConnectionResourceCreate5xxAdoptsByName(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) { s.postStatus = http.StatusBadGateway })
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnRefusal(vpnConnPlanTestBadGateway)},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.postBodies) != 1 {
						t.Errorf("Expected the POST to be sent once, got %d", len(state.postBodies))
					}
					state.postStatus = 0
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// plantOtherRow stands a connection with the planned name on the VPC. The POST
// then leaves it alone and answers 502.
func plantOtherRow(state *vpnConnPlanTestServerState, peer string, age time.Duration) {
	state.set(func(s *vpnConnPlanTestServerState) {
		s.exists = true
		s.keepRow = true
		s.postStatus = http.StatusBadGateway
		s.row = vpnConnPlanTestRowFromBody(map[string]any{
			"name": "branch-office", "peerAddress": peer, "peerCidrs": []string{"192.168.10.0/24"},
		}, "connected")
		s.row["createdAt"] = time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	})
}

func clearPlantedRow(state *vpnConnPlanTestServerState) {
	state.set(func(s *vpnConnPlanTestServerState) {
		s.exists = false
		s.keepRow = false
		s.postStatus = 0
	})
}

// A 5xx answer can come from a request that made no row, while a connection of
// the same name from an earlier apply stands on the VPC. The create does not
// adopt it, saves no state, and names the import command.
func TestVpcVpnConnectionResourceCreate5xxDoesNotAdoptOldRow(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	plantOtherRow(state, "203.0.113.10", time.Hour)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ExpectError: vpnConnRefusal("A VPN connection named branch-office already exists on this VPC that this apply didn't create. " +
					"Import it with: terraform import gpcn_vpc_vpn_connection.<name> " + vpnConnPlanTestVpcID + "/" + vpnConnPlanTestConnID),
			},
			{
				PreConfig: func() { clearPlantedRow(state) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A fresh row of the same name that points at another peer is not the row of
// this request either.
func TestVpcVpnConnectionResourceCreate5xxDoesNotAdoptOtherPeer(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	plantOtherRow(state, "203.0.113.99", 0)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnRefusal("that this apply didn't create")},
			{
				PreConfig: func() { clearPlantedRow(state) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A lookup that fails after a 5xx answer cannot tell whether a row exists. The
// error says so and keeps the lookup error.
func TestVpcVpnConnectionResourceCreate5xxLookupFailsNamesTheRisk(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) {
		s.keepRow = true
		s.postStatus = http.StatusBadGateway
		s.listStatus = http.StatusForbidden
	})
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ExpectError: vpnConnDetailOrder(vpnConnPlanTestBadGateway,
					"A VPN connection may have been created; check the VPC and import it if so."),
			},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) { s.keepRow = false; s.postStatus = 0; s.listStatus = 0 })
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// An adopted row that still runs a job is read again once the job ends.
func TestVpcVpnConnectionResourceCreate5xxPollsAdoptedJob(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) {
		s.postStatus = http.StatusBadGateway
		s.createdState = "creating"
		s.postActiveJob = "job-live"
	})
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnRefusal(vpnConnPlanTestBadGateway)},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					if state.row["activeJobId"] != nil {
						t.Errorf("Expected the create to poll the job of the adopted row, which still runs %v", state.row["activeJobId"])
					}
					state.postStatus = 0
					state.postActiveJob = ""
					state.createdState = "connecting"
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// A job that ended, followed by a detail read that fails, still leaves a row.
// The create saves the planned values and the ID with the error, so Terraform
// taints the resource.
func TestVpcVpnConnectionResourcePostJobGetFailsKeepsRow(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) { s.itemStatus = http.StatusForbidden })
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: vpnConnRefusal(vpnConnPlanTestGetRefused)},
			{
				PreConfig: func() { state.set(func(s *vpnConnPlanTestServerState) { s.itemStatus = 0 }) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// The API saves the values of an update before its job runs. After a failed
// job a refresh shows the saved values and no change, yet the data center runs
// the old ones. The plan updates the connection, and the PUT sends every
// attribute that is not a label.
func TestVpcVpnConnectionResourceUnappliedPlansUpdate(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	allKeys := []string{"dhGroup", "dpd", "encryption", "ikeLifetimeSeconds", "ikeVersion", "integrity", "ipsecLifetimeSeconds", "peerAddress", "peerCidrs", "pfsGroup"}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) {
						s.desiredApplied = false
						s.row["state"] = "connected"
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					puts := state.putKeys()
					if len(puts) != 1 || !slices.Equal(puts[0], allKeys) {
						return fmt.Errorf("expected one PUT with the keys %v, got %v", allKeys, puts)
					}
					return nil
				},
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// A failed update parks the row as failed with desiredApplied false, and the
// retried job settles it. The plan must mark every attribute that the job
// rewrites unknown, or Terraform rejects the apply as inconsistent.
func TestVpcVpnConnectionResourceParkedFailedRetrySettles(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) {
						s.desiredApplied = false
						s.putSettles = true
						s.row["state"] = "failed"
						s.row["failureReason"] = vpnConnPlanTestParkedText
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue(gpcnVpcVpnConnectionTest, tfjsonpath.New("state")),
						plancheck.ExpectUnknownValue(gpcnVpcVpnConnectionTest, tfjsonpath.New("failure_reason")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "state", "connecting"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnConnectionTest, "failure_reason"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "desired_applied", "true"),
				),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// Saved settings that do not run call for an update only when the connection
// is idle. While it is being created or removed, or while a job runs, the plan
// stays empty.
func TestVpcVpnConnectionResourceUnappliedWhileBusyPlansNoUpdate(t *testing.T) {
	t.Parallel()
	for name, busy := range map[string]func(*vpnConnPlanTestServerState){
		"creating":       func(s *vpnConnPlanTestServerState) { s.row["state"] = "creating" },
		"removing":       func(s *vpnConnPlanTestServerState) { s.row["state"] = "removing" },
		"job is running": func(s *vpnConnPlanTestServerState) { s.row["state"] = "connected"; s.row["activeJobId"] = "job-live" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, state := startVpnConnPlanMockServer(t)
			config := newVpnConnPlanTestConfig().render(server.URL)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config},
					{
						PreConfig: func() {
							state.set(func(s *vpnConnPlanTestServerState) {
								s.desiredApplied = false
								busy(s)
							})
						},
						Config:   config,
						PlanOnly: true,
					},
				},
			})
		})
	}
}

// A failed connection that no longer accepts an update can only be deleted. The
// plan replaces it, where an update would be refused.
func TestVpcVpnConnectionResourceFailedWithoutUpdatePlansReplace(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					state.set(func(s *vpnConnPlanTestServerState) {
						s.desiredApplied = false
						s.row["state"] = "failed"
						s.row["failureReason"] = vpnConnPlanTestFailReason
						s.row["acceptedVerbs"] = []string{"delete"}
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionReplace)},
				},
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.putBodies) != 0 || state.deleteCount != 1 || len(state.postBodies) != 2 {
						return fmt.Errorf("expected a delete and a second POST with no PUT, got %d PUTs, %d deletes, %d POSTs",
							len(state.putBodies), state.deleteCount, len(state.postBodies))
					}
					return nil
				},
			},
		},
	})
}

// A vpn_gateway_id that is not the gateway of the VPC stops the create before
// the POST.
func TestVpcVpnConnectionResourceWrongGateway(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	cfg := newVpnConnPlanTestConfig()
	cfg.GatewayID = vpnConnPlanTestOtherGWID

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg.render(server.URL),
				ExpectError: vpnConnRefusal(fmt.Sprintf("vpn_gateway_id %s is not the VPN gateway of VPC %s",
					vpnConnPlanTestOtherGWID, vpnConnPlanTestVpcID)),
			},
			{
				Config: newVpnConnPlanTestConfig().render(server.URL),
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.postBodies) != 0 {
						t.Errorf("Expected no POST before the gateway check passed, got %d", len(state.postBodies))
					}
				},
			},
		},
	})
}

// Every field the API stores changes in place. The PUT carries that field alone.
func TestVpcVpnConnectionResourceUpdateInPlace(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)

	type change struct {
		name   string
		key    string
		mutate func(*vpnConnPlanTestConfig)
	}
	changes := []change{
		{"name", "name", func(c *vpnConnPlanTestConfig) { c.Name = "branch-office-2" }},
		{"description", "description", func(c *vpnConnPlanTestConfig) { c.Desc = "Chicago office" }},
		{"description cleared", "description", func(c *vpnConnPlanTestConfig) { c.Desc = "" }},
		{"peer_address", "peerAddress", func(c *vpnConnPlanTestConfig) { c.Peer = "203.0.113.11" }},
		{"peer_cidrs", "peerCidrs", func(c *vpnConnPlanTestConfig) { c.Cidrs = `["192.168.10.0/24", "192.168.20.0/24"]` }},
		{"ike_version", "ikeVersion", func(c *vpnConnPlanTestConfig) { c.Ike = "v1" }},
		{"encryption", "encryption", func(c *vpnConnPlanTestConfig) { c.Encryption = "aes128" }},
		{"integrity", "integrity", func(c *vpnConnPlanTestConfig) { c.Integrity = "sha512" }},
		{"dh_group", "dhGroup", func(c *vpnConnPlanTestConfig) { c.Dh = 5 }},
		{"pfs_group", "pfsGroup", func(c *vpnConnPlanTestConfig) { c.Pfs = 2 }},
		{"ike_lifetime_seconds", "ikeLifetimeSeconds", func(c *vpnConnPlanTestConfig) { c.IkeLife = 14400 }},
		{"ipsec_lifetime_seconds", "ipsecLifetimeSeconds", func(c *vpnConnPlanTestConfig) { c.IpsecLife = 1800 }},
		{"dpd", "dpd", func(c *vpnConnPlanTestConfig) { c.Dpd = false }},
	}

	cfg := newVpnConnPlanTestConfig()
	steps := []resource.TestStep{{Config: cfg.render(server.URL)}}
	for index, step := range changes {
		step.mutate(&cfg)
		expected := step.key
		wantPuts := index + 1
		steps = append(steps, resource.TestStep{
			Config: cfg.render(server.URL),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate),
				},
			},
			Check: func(*terraform.State) error {
				puts := state.putKeys()
				if len(puts) != wantPuts {
					return fmt.Errorf("%s: expected %d PUTs, got %d", step.name, wantPuts, len(puts))
				}
				if last := puts[len(puts)-1]; len(last) != 1 || last[0] != expected {
					return fmt.Errorf("%s: expected the PUT to carry only %s, got %v", step.name, expected, last)
				}
				if step.name == "description cleared" {
					state.mu.Lock()
					defer state.mu.Unlock()
					if value, found := state.putBodies[len(state.putBodies)-1]["description"]; !found || value != nil {
						return fmt.Errorf("expected the PUT to send description as null, got %v", value)
					}
				}
				return nil
			},
		})
	}
	steps = append(steps, resource.TestStep{Config: cfg.render(server.URL), PlanOnly: true})

	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: testProtoV6ProviderFactories, Steps: steps})
}

// A new psk_wo_version sends the psk_wo value, and nothing else.
func TestVpcVpnConnectionResourcePskWoVersionSendsPsk(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	cfg := newVpnConnPlanTestConfig()
	rotated := cfg
	rotated.PskLine = fmt.Sprintf("psk_wo = %q\n  psk_wo_version = 2", vpnConnPlanTestPskTwo)
	silent := rotated
	silent.PskLine = fmt.Sprintf("psk_wo = %q\n  psk_wo_version = 2", vpnConnPlanTestPsk)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg.render(server.URL)},
			{
				Config: rotated.render(server.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.putBodies) != 1 {
						return fmt.Errorf("expected one PUT, got %d", len(state.putBodies))
					}
					body := state.putBodies[0]
					if len(body) != 1 || body["psk"] != vpnConnPlanTestPskTwo {
						return fmt.Errorf("expected the PUT to carry only the new psk_wo value, got %v", body)
					}
					return nil
				},
			},
			// The value of a write-only attribute changes, the version does not.
			{
				Config: silent.render(server.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					if puts := state.putKeys(); len(puts) != 1 {
						return fmt.Errorf("expected no new PUT, got %d in all", len(puts))
					}
					return nil
				},
			},
		},
	})
}

// A new psk sends the key and nothing else.
func TestVpcVpnConnectionResourcePskChangeSendsOnlyPsk(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	cfg := newVpnConnPlanTestConfig()
	cfg.PskLine = fmt.Sprintf("psk = %q", vpnConnPlanTestPsk)
	changed := cfg
	changed.PskLine = fmt.Sprintf("psk = %q", vpnConnPlanTestPskTwo)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg.render(server.URL),
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if state.postBodies[0]["psk"] != vpnConnPlanTestPsk {
						return fmt.Errorf("expected the POST to carry psk, got %v", state.postBodies[0]["psk"])
					}
					return nil
				},
			},
			{
				Config: changed.render(server.URL),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.putBodies) != 1 {
						return fmt.Errorf("expected one PUT, got %d", len(state.putBodies))
					}
					body := state.putBodies[0]
					if len(body) != 1 || body["psk"] != vpnConnPlanTestPskTwo {
						return fmt.Errorf("expected the PUT to carry only the new psk, got %v", body)
					}
					return nil
				},
			},
			{Config: changed.render(server.URL), PlanOnly: true},
		},
	})
}

// Exactly one of psk and psk_wo is set, psk_wo needs its version, and a key
// that fails the format never appears in the message.
func TestVpcVpnConnectionResourcePskExactlyOne(t *testing.T) {
	t.Parallel()
	server, _ := startVpnConnPlanMockServer(t)

	both := newVpnConnPlanTestConfig()
	both.PskLine = fmt.Sprintf("psk = %q\n  psk_wo = %q\n  psk_wo_version = 1", vpnConnPlanTestPsk, vpnConnPlanTestPsk)
	neither := newVpnConnPlanTestConfig()
	neither.PskLine = ""
	noVersion := newVpnConnPlanTestConfig()
	noVersion.PskLine = fmt.Sprintf("psk_wo = %q", vpnConnPlanTestPsk)
	versionOnly := newVpnConnPlanTestConfig()
	versionOnly.PskLine = "psk_wo_version = 1"
	badKey := newVpnConnPlanTestConfig()
	badKey.PskLine = `psk = "has a space in the middle"`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: both.render(server.URL), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*2\s+attributes\s+specified\s+when\s+one\s+\(and\s+only\s+one\)`)},
			{Config: neither.render(server.URL), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*No\s+attribute\s+specified\s+when\s+one\s+\(and\s+only\s+one\)`)},
			{Config: noVersion.render(server.URL), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*psk_wo_version`)},
			{Config: versionOnly.render(server.URL), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination.*psk_wo`)},
			{
				Config: badKey.render(server.URL), PlanOnly: true,
				ExpectError: regexp.MustCompile(`(?s)Invalid pre-shared key.*psk\s+must\s+be\s+16.64\s+printable`),
			},
		},
	})
}

// A CIDR with host bits set is refused at plan time. The message names the
// network form the API would store.
func TestVpcVpnConnectionResourceHostBitsRefused(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	cfg := newVpnConnPlanTestConfig()
	cfg.Cidrs = `["192.168.10.7/24"]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg.render(server.URL), PlanOnly: true,
				ExpectError: vpnConnRefusal("peer_cidrs must be written as its network address. '192.168.10.7/24' has host bits set. Use '192.168.10.0/24'"),
			},
			{
				Config: newVpnConnPlanTestConfig().render(server.URL),
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					if len(state.postBodies) != 0 {
						t.Errorf("Expected no POST for a refused plan, got %d", len(state.postBodies))
					}
				},
			},
		},
	})
}

// The import ID is "<vpc_id>/<connection_id>". The key attributes stay null.
func TestVpcVpnConnectionResourceImport(t *testing.T) {
	t.Parallel()
	server, _ := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config:                  config,
				ResourceName:            gpcnVpcVpnConnectionTest,
				ImportState:             true,
				ImportStateId:           vpnConnPlanTestVpcID + "/" + vpnConnPlanTestConnID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"psk_wo_version"},
			},
			{
				Config:        config,
				ResourceName:  gpcnVpcVpnConnectionTest,
				ImportState:   true,
				ImportStateId: vpnConnPlanTestConnID,
				ExpectError:   vpnConnRefusal("Import ID must be '<vpc_id>/<connection_id>'"),
			},
		},
	})
}

// A connection removed outside Terraform answers 404. Read drops it, and the
// next plan creates it again.
func TestVpcVpnConnectionResourceGoneDropsState(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { state.set(func(s *vpnConnPlanTestServerState) { s.exists = false }) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

// A refused delete shows the API's sentence unchanged.
func TestVpcVpnConnectionResourceDeleteStateConflict(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(*terraform.State) error {
					state.set(func(s *vpnConnPlanTestServerState) { s.deleteConflict = vpnConnPlanTestConflict })
					return nil
				},
			},
			{Config: config, Destroy: true, ExpectError: vpnConnRefusal(vpnConnPlanTestConflict)},
		},
	})
}

// A delete that meets a removal already under way waits until the API answers
// 404, instead of failing on the 409.
func TestVpcVpnConnectionResourceDeleteAlreadyRemovingWaits(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	config := newVpnConnPlanTestConfig().render(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.getsAfterDel != 3 {
				return fmt.Errorf("expected 3 reads after the 409 (two removing, one 404), got %d", state.getsAfterDel)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(*terraform.State) error {
					state.set(func(s *vpnConnPlanTestServerState) { s.deleteRemoving = true; s.removingHold = 2 })
					return nil
				},
			},
		},
	})
}

// A failed update job keeps the prior state. The values the API saved show up
// on the next refresh, and the W1 warning tells the operator to apply again.
func TestVpcVpnConnectionResourceUpdateJobFailedKeepsState(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	cfg := newVpnConnPlanTestConfig()
	renamed := cfg
	renamed.Name = "branch-office-2"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg.render(server.URL)},
			{
				PreConfig:   func() { state.set(func(s *vpnConnPlanTestServerState) { s.failJob = true }) },
				Config:      renamed.render(server.URL),
				ExpectError: vpnConnRefusal(vpnConnPlanTestJobFailText),
			},
		},
	})
}

// vpnConnDirectCall builds the pieces the framework would pass to a CRUD method.
func vpnConnDirectCall(t *testing.T, host string) (*vpcVpnConnectionResource, tfsdk.State) {
	t.Helper()
	cfg := client.DefaultConfig(host, "test-key")
	cfg.MaxRetries = 0
	gpcnClient, err := client.NewGpcnClient(cfg)
	if err != nil {
		t.Fatalf("failed to create GPCN client: %v", err)
	}
	r := &vpcVpnConnectionResource{client: gpcnClient}

	var schemaResp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &schemaResp)
	empty := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(context.Background()), nil)}
	return r, empty
}

func vpnConnDirectModel(connectionID string) vpcvpnconnections.ResourceModel {
	cidrs := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("192.168.10.0/24")})
	return vpcvpnconnections.ResourceModel{
		ID: types.StringValue(connectionID), VpcID: types.StringValue(vpnConnPlanTestVpcID),
		VpnGatewayID: types.StringValue(vpnConnPlanTestGatewayID), Name: types.StringValue("branch-office"),
		Description: types.StringValue(""), PeerAddress: types.StringValue("203.0.113.10"), PeerCidrs: cidrs,
		Psk: types.StringNull(), PskWo: types.StringNull(), PskWoVersion: types.Int64Value(1),
		IkeVersion: types.StringValue("v2"), Encryption: types.StringValue("aes256"), Integrity: types.StringValue("sha256"),
		DhGroup: types.Int64Value(14), PfsGroup: types.Int64Value(14),
		IkeLifetimeSeconds: types.Int64Value(28800), IpsecLifetimeSeconds: types.Int64Value(3600), Dpd: types.BoolValue(true),
		State: types.StringValue("connected"), FailureReason: types.StringNull(), DesiredApplied: types.BoolValue(true),
		GatewayKind: types.StringValue("router"), LocalEndpointAddress: types.StringValue(vpnConnPlanTestLocalEnd),
		LocalNetworks: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("10.121.1.0/24")}),
	}
}

// Read warns with the W1 bytes when the saved settings are not running and no
// job is active. It stays quiet while a job runs.
func TestVpcVpnConnectionResourceDesiredAppliedWarning(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	state.set(func(s *vpnConnPlanTestServerState) {
		s.exists = true
		s.row = vpnConnPlanTestRowFromBody(map[string]any{
			"name": "branch-office", "peerAddress": "203.0.113.10", "peerCidrs": []string{"192.168.10.0/24"},
		}, "connected")
		s.desiredApplied = false
	})
	r, empty := vpnConnDirectCall(t, server.URL)

	prior := empty
	model := vpnConnDirectModel(vpnConnPlanTestConnID)
	if diags := prior.Set(context.Background(), &model); diags.HasError() {
		t.Fatalf("failed to build state: %v", diags)
	}

	read := func() *fwresource.ReadResponse {
		resp := &fwresource.ReadResponse{State: prior}
		r.Read(context.Background(), fwresource.ReadRequest{State: prior}, resp)
		return resp
	}

	resp := read()
	if resp.Diagnostics.HasError() {
		t.Fatalf("Expected Read to succeed, got %v", resp.Diagnostics)
	}
	warnings := resp.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("Expected one warning, got %d: %v", len(warnings), resp.Diagnostics)
	}
	const w1 = "The VPN connection's saved settings are not yet running at the data center. Apply again to retry."
	if warnings[0].Detail() != w1 {
		t.Errorf("Expected the W1 bytes %q, got %q", w1, warnings[0].Detail())
	}

	state.set(func(s *vpnConnPlanTestServerState) { s.row["activeJobId"] = "job-live" })
	if warnings := read().Diagnostics.Warnings(); len(warnings) != 0 {
		t.Errorf("Expected no warning while a job is active, got %v", warnings)
	}

	for _, busy := range []string{"creating", "removing"} {
		state.set(func(s *vpnConnPlanTestServerState) { s.row["activeJobId"] = nil; s.row["state"] = busy })
		if warnings := read().Diagnostics.Warnings(); len(warnings) != 0 {
			t.Errorf("Expected no warning while the state is %s, got %v", busy, warnings)
		}
	}

	state.set(func(s *vpnConnPlanTestServerState) { s.row["state"] = "connected"; s.desiredApplied = true })
	if warnings := read().Diagnostics.Warnings(); len(warnings) != 0 {
		t.Errorf("Expected no warning when the settings run, got %v", warnings)
	}
}

// The key never reaches the logs, on create or on update, from either source.
func TestVpcVpnConnectionResourcePskNotLogged(t *testing.T) {
	t.Parallel()
	server, state := startVpnConnPlanMockServer(t)
	r, empty := vpnConnDirectCall(t, server.URL)

	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)

	build := func(configure func(*vpcvpnconnections.ResourceModel)) tfsdk.State {
		built := empty
		model := vpnConnDirectModel(vpnConnPlanTestConnID)
		configure(&model)
		if diags := built.Set(ctx, &model); diags.HasError() {
			t.Fatalf("failed to build state: %v", diags)
		}
		return built
	}

	createPlan := build(func(m *vpcvpnconnections.ResourceModel) {
		m.ID = types.StringUnknown()
		m.Psk = types.StringValue(vpnConnPlanTestPsk)
		m.PskWoVersion = types.Int64Null()
		m.State = types.StringUnknown()
		m.FailureReason = types.StringUnknown()
		m.DesiredApplied = types.BoolUnknown()
		m.GatewayKind = types.StringUnknown()
		m.LocalEndpointAddress = types.StringUnknown()
		m.LocalNetworks = types.ListUnknown(types.StringType)
	})
	createResp := &fwresource.CreateResponse{State: empty}
	r.Create(ctx, fwresource.CreateRequest{Plan: tfsdk.Plan(createPlan), Config: tfsdk.Config(createPlan)}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Expected the create to succeed, got %v", createResp.Diagnostics)
	}

	// Update by psk_wo: the key lives in the configuration only.
	state.set(func(s *vpnConnPlanTestServerState) { s.failJob = false })
	priorState := createResp.State
	updatePlan := build(func(m *vpcvpnconnections.ResourceModel) {
		m.PskWoVersion = types.Int64Value(2)
		m.State = types.StringUnknown()
		m.FailureReason = types.StringUnknown()
		m.DesiredApplied = types.BoolUnknown()
		m.LocalNetworks = types.ListUnknown(types.StringType)
	})
	updateConfig := build(func(m *vpcvpnconnections.ResourceModel) {
		m.PskWo = types.StringValue(vpnConnPlanTestPskTwo)
		m.PskWoVersion = types.Int64Value(2)
	})
	updateResp := &fwresource.UpdateResponse{State: priorState}
	r.Update(ctx, fwresource.UpdateRequest{
		Plan:   tfsdk.Plan(updatePlan),
		State:  priorState,
		Config: tfsdk.Config(updateConfig),
	}, updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("Expected the update to succeed, got %v", updateResp.Diagnostics)
	}

	// Update by psk.
	pskPlan := build(func(m *vpcvpnconnections.ResourceModel) {
		m.Psk = types.StringValue(vpnConnPlanTestPsk)
		m.State = types.StringUnknown()
		m.FailureReason = types.StringUnknown()
		m.DesiredApplied = types.BoolUnknown()
		m.LocalNetworks = types.ListUnknown(types.StringType)
	})
	pskResp := &fwresource.UpdateResponse{State: updateResp.State}
	r.Update(ctx, fwresource.UpdateRequest{
		Plan:   tfsdk.Plan(pskPlan),
		State:  updateResp.State,
		Config: tfsdk.Config(pskPlan),
	}, pskResp)
	if pskResp.Diagnostics.HasError() {
		t.Fatalf("Expected the psk update to succeed, got %v", pskResp.Diagnostics)
	}

	state.mu.Lock()
	sentCreate := len(state.postBodies) == 1 && state.postBodies[0]["psk"] == vpnConnPlanTestPsk
	sentUpdates := len(state.putBodies) == 2
	state.mu.Unlock()
	if !sentCreate || !sentUpdates {
		t.Fatalf("Expected the calls to reach the API with the key (create %v, two PUTs %v)", sentCreate, sentUpdates)
	}

	if logs.Len() == 0 {
		t.Fatal("Expected the captured log to hold the provider's messages")
	}
	for _, secret := range []string{vpnConnPlanTestPsk, vpnConnPlanTestPskTwo} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("Expected the log to omit the key %q, got:\n%s", secret, logs.String())
		}
	}
}
