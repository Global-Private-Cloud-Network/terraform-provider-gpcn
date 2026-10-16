package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/vpcnsgs"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	nsgPlanTestVpcID        = "55555555-5555-4555-8555-555555555555"
	nsgPlanTestID           = "66666666-6666-4666-8666-666666666666"
	nsgPlanTestTimestamp    = "2026-01-02T15:04:05Z"
	nsgPlanTestFailedReason = "the provider rejected the group"
	gpcnVpcNsgTest          = "gpcn_vpc_nsg.test"
)

// regexpNsgCreatedButReadBackFailed pins the R155 sentence for a group the
// platform built and then would not serve back.
var regexpNsgCreatedButReadBackFailed = regexp.MustCompile(`(?s)security\s+group\s+` + nsgPlanTestID +
	`\s+was\s+created\s+and\s+is\s+in\s+state,\s+but\s+reading\s+it\s+back\s+failed.*` +
	`Terraform\s+has\s+marked\s+the\s+group\s+tainted,\s+so\s+the\s+next\s+apply\s+deletes\s+it\s+and\s+creates\s+it\s+again\.`)

// regexpNsgCreatedButJobFailed pins the R145 sentence for a group build the
// platform gave up on after it inserted the row.
var regexpNsgCreatedButJobFailed = regexp.MustCompile(`(?s)security\s+group\s+` + nsgPlanTestID +
	`\s+was\s+created\s+and\s+is\s+in\s+state,\s+but\s+its\s+creation\s+job\s+failed.*` +
	`Terraform\s+has\s+marked\s+the\s+group\s+tainted,\s+so\s+the\s+next\s+apply\s+deletes\s+it\s+and\s+creates\s+it\s+again\.`)

const (
	nsgPlanTestRuleHTTPS = `
  rule {
    direction      = "ingress"
    protocol       = "tcp"
    port_range_min = 443
    port_range_max = 443
    remote_cidr    = "0.0.0.0/0"
    description    = "https"
  }`
	nsgPlanTestRulePing = `
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = "10.60.0.0/16"
  }`
)

// nsgPlanTestServerState is the group the mock keeps between requests. It
// stores the rule bodies it was sent. A read after an apply then agrees with
// the configuration and leaves the refresh plan empty.
type nsgPlanTestServerState struct {
	mu           sync.Mutex
	name         string
	description  string
	isDefault    bool
	rules        []map[string]any
	lastRulesPut map[string]any
	rulesPuts    int
	renamePuts   int
	subnetCount  int64
	refuseDelete bool
	// subnetCountOnRename stands for a subnet that binds between the refresh
	// and the apply. Zero leaves the census alone.
	subnetCountOnRename int64
	// missingOnDelete answers the DELETE with a 404. A group another operator
	// already removed gives that answer.
	missingOnDelete bool
	deleted         bool
	// failCreateJob makes the build job stop badly. GPCN inserts the group
	// before it dispatches that job, so the group exists either way.
	failCreateJob bool
	// rowState is the lifecycle state the group reads back in. A group parks
	// in "failed" when its build job or a rule replace stops badly.
	rowState string
	// failureReason stands for the sentence GPCN files against a parked group.
	// An empty value reads back as the API's null.
	failureReason string
	// emptyFailureReason renders the reason as "" rather than null. The
	// provider treats both as no reason, and only this switch reaches the
	// empty-string half of that guard.
	emptyFailureReason bool
	// failNextRead answers the next group read with a 500. The build still
	// ran, so the group is there and only the read-back fails.
	failNextRead bool
	// failReadAt makes the Nth group read answer with a 500. Zero turns it off.
	failReadAt int
	reads      int
	// loseVpnOwnership makes a rules PUT strip the owner from a re-sent VPN
	// rule. A rename of the connection that races the replace does the same.
	loseVpnOwnership bool
}

// nsgPlanTestRuleKey is the content key GPCN diffs a rules PUT by.
func nsgPlanTestRuleKey(rule map[string]any) string {
	return fmt.Sprint(rule["direction"], ":", rule["protocol"], ":", rule["portRangeMin"], ":", rule["portRangeMax"], ":", rule["remoteCidr"])
}

// addVpnRule files a rule the way a VPN connection does: an ordinary row with
// the connection ID as its owner.
func (s *nsgPlanTestServerState) addVpnRule(connectionID, remoteCidr, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, map[string]any{
		"id":              "rule-vpn-" + remoteCidr,
		"direction":       "ingress",
		"protocol":        "all",
		"remoteCidr":      remoteCidr,
		"description":     description,
		"vpnConnectionId": connectionID,
	})
}

func (s *nsgPlanTestServerState) storeRules(body map[string]any) {
	type owner struct{ connectionID, description string }
	owners := map[string]owner{}
	for _, rule := range s.rules {
		if connectionID, _ := rule["vpnConnectionId"].(string); connectionID != "" {
			description, _ := rule["description"].(string)
			owners[nsgPlanTestRuleKey(rule)] = owner{connectionID, description}
		}
	}

	sent, _ := body["rules"].([]any)
	stored := make([]map[string]any, 0, len(sent))
	for index, entry := range sent {
		sentRule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		// Copy the rule, so the recorded PUT body stays as the provider sent it.
		rule := make(map[string]any, len(sentRule)+2)
		for key, value := range sentRule {
			rule[key] = value
		}
		rule["id"] = fmt.Sprintf("rule-%d", index)
		// The API keeps the owner only for a rule sent back unchanged.
		if held, ok := owners[nsgPlanTestRuleKey(rule)]; ok && !s.loseVpnOwnership {
			if description, _ := rule["description"].(string); description == held.description {
				rule["vpnConnectionId"] = held.connectionID
			}
		}
		stored = append(stored, rule)
	}
	s.rules = stored
}

func (s *nsgPlanTestServerState) detail() map[string]any {
	var description any
	if s.description != "" {
		description = s.description
	}
	var failureReason any
	if s.failureReason != "" {
		failureReason = s.failureReason
	} else if s.emptyFailureReason {
		failureReason = ""
	}
	return map[string]any{
		"nsg": map[string]any{
			"id":            nsgPlanTestID,
			"name":          s.name,
			"description":   description,
			"isDefault":     s.isDefault,
			"state":         s.rowState,
			"failureReason": failureReason,
			"ruleCount":     len(s.rules),
			"subnetCount":   s.subnetCount,
			"activeJobId":   nil,
			"createdAt":     nsgPlanTestTimestamp,
			"updatedAt":     nsgPlanTestTimestamp,
		},
		"rules": s.rules,
	}
}

func startNsgPlanMockServer(t *testing.T) (*httptest.Server, *nsgPlanTestServerState) {
	t.Helper()

	state := &nsgPlanTestServerState{rules: []map[string]any{}, rowState: "ready"}

	collection := "/v1/resource/vpcs/" + nsgPlanTestVpcID + "/nsgs/"
	groupPath := collection + nsgPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == collection:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.name, _ = body["name"].(string)
			state.description, _ = body["description"].(string)
			state.storeRules(body)
			state.deleted = false
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"nsgId": nsgPlanTestID, "jobId": "job-create"},
			})
		case r.Method == http.MethodGet && r.URL.Path == groupPath:
			state.mu.Lock()
			state.reads++
			failRead := state.failNextRead || state.reads == state.failReadAt
			state.failNextRead = false
			deleted := state.deleted
			detail := state.detail()
			state.mu.Unlock()
			if failRead {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"success":false,"message":"Internal server error","error":{"code":"Internal Error","statusCode":500,"details":null}}`)
				return
			}
			if deleted {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"Security group not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": detail})
		case r.Method == http.MethodPut && r.URL.Path == groupPath+"/rules":
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.lastRulesPut = body
			state.rulesPuts++
			state.storeRules(body)
			state.mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-rules", "Operation initiated successfully")
		case r.Method == http.MethodPut && r.URL.Path == groupPath:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.renamePuts++
			state.name, _ = body["name"].(string)
			state.description, _ = body["description"].(string)
			if state.subnetCountOnRename > 0 {
				state.subnetCount = state.subnetCountOnRename
			}
			detail := state.detail()
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": detail["nsg"]})
		case r.Method == http.MethodDelete && r.URL.Path == groupPath:
			state.mu.Lock()
			missing := state.missingOnDelete
			refuse := state.refuseDelete
			if missing {
				state.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"Security group not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
				return
			}
			if refuse {
				state.refuseDelete = false
			} else {
				state.deleted = true
			}
			state.mu.Unlock()
			if refuse {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"success":false,"message":"The VPC's default security group cannot be deleted while it is the default","error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`)
				return
			}
			testutil.HandleCreateJobResponse(w, "job-delete", "Operation initiated successfully")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			nsgPlanTestHandleJob(w, r, state)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

// failTheCreateJob arms the build failure under the lock. The mock server
// reads the flag from another goroutine.
func (s *nsgPlanTestServerState) failTheCreateJob() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCreateJob = true
}

// failTheNextRead arms the read failure under the lock. The mock server reads
// the flag from another goroutine.
func (s *nsgPlanTestServerState) failTheNextRead() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNextRead = true
}

// nsgPlanTestHandleJob answers the poll for the job the request names. Only
// the build job can stop badly, so every other job completes.
func nsgPlanTestHandleJob(w http.ResponseWriter, r *http.Request, state *nsgPlanTestServerState) {
	jobID := "job-1"
	if ids, ok := testutil.ReadRequestBody(r)["jobIds"].([]any); ok && len(ids) > 0 {
		jobID, _ = ids[0].(string)
	}

	state.mu.Lock()
	failCreate := state.failCreateJob
	state.mu.Unlock()

	if jobID == "job-create" && failCreate {
		testutil.WriteJSONResponse(w, map[string]any{
			"success": true,
			"message": "Job status retrieved",
			"data": map[string]any{"jobs": []map[string]any{{
				"jobId":        jobID,
				"isCompleted":  false,
				"isTerminal":   true,
				"hasFailed":    true,
				"errorMessage": nsgPlanTestFailedReason,
			}}},
		})
		return
	}

	testutil.HandleJobResponse(w, jobID, nsgPlanTestID, true)
}

func nsgPlanTestConfig(host, name, rules string) string {
	return nsgPlanTestConfigWithDescription(host, name, "", rules)
}

// nsgPlanTestConfigWithoutRetries drops the retry budget. The client retries a
// 500 answer, which would hide a read-back that fails once.
func nsgPlanTestConfigWithoutRetries(host, name, rules string) string {
	config := nsgPlanTestConfig(host, name, rules)
	return strings.Replace(config, "  api_key = \"test-key\"\n", "  api_key = \"test-key\"\n  max_retries = 0\n", 1)
}

func nsgPlanTestConfigWithDescription(host, name, description, rules string) string {
	if description != "" {
		description = fmt.Sprintf("  description = %q\n", description)
	}
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_nsg" "test" {
  vpc_id = %q
  name   = %q
%s%s
}
`, host, nsgPlanTestVpcID, name, description, rules)
}

// The rules PUT is a full replace. A configuration that drops one rule must
// still send every rule it keeps. A rename must not touch the rules at all.
func TestVpcNsgResourcePlanCreateRenameAndReplaceRules(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	rulesPutCount := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.rulesPuts != want {
				return fmt.Errorf("expected %d rules PUT calls, got %d", want, state.rulesPuts)
			}
			return nil
		}
	}

	lastRulesPutHolds := func(wantCount int, wantProtocol string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.lastRulesPut == nil {
				return fmt.Errorf("expected a rules PUT, got none")
			}
			sent, ok := state.lastRulesPut["rules"].([]any)
			if !ok {
				return fmt.Errorf("expected a rules array in the body, got %T", state.lastRulesPut["rules"])
			}
			if len(sent) != wantCount {
				return fmt.Errorf("expected the complete set of %d rules, got %d", wantCount, len(sent))
			}
			for _, entry := range sent {
				rule, _ := entry.(map[string]any)
				if protocol, _ := rule["protocol"].(string); protocol == wantProtocol {
					return nil
				}
			}
			return fmt.Errorf("expected a %s rule in the replace body, got %v", wantProtocol, sent)
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "id", nsgPlanTestID),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "name", "nsg-plan-a"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "description", ""),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "is_default", "false"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule_count", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "subnet_count", "0"),
					// A group that never failed sends a null reason, and the
					// state must keep it null.
					resource.TestCheckNoResourceAttr(gpcnVpcNsgTest, "failure_reason"),
					// The rules travel inline with the create, so no replace
					// call is needed to put them there.
					rulesPutCount(0),
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-b", nsgPlanTestRuleHTTPS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "name", "nsg-plan-b"),
					rulesPutCount(0),
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-b", nsgPlanTestRuleHTTPS+nsgPlanTestRulePing),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					rulesPutCount(1),
					lastRulesPutHolds(2, "icmp"),
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-b", nsgPlanTestRulePing),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "1"),
					rulesPutCount(2),
					// The removal is expressed by what the body still carries.
					lastRulesPutHolds(1, "icmp"),
				),
			},
			{
				ResourceName:      gpcnVpcNsgTest,
				ImportState:       true,
				ImportStateId:     nsgPlanTestVpcID + "/" + nsgPlanTestID,
				ImportStateVerify: true,
			},
		},
	})
}

// Ports belong to tcp and udp only. The plan refuses the rule in the same
// words the API would have.
func TestVpcNsgResourcePlanRefusesPortsOnIcmp(t *testing.T) {
	t.Parallel()
	server, _ := startNsgPlanMockServer(t)

	badRule := `
  rule {
    direction      = "ingress"
    protocol       = "icmp"
    port_range_min = 1
    port_range_max = 2
    remote_cidr    = "0.0.0.0/0"
  }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      nsgPlanTestConfig(server.URL, "nsg-plan-a", badRule),
				ExpectError: regexp.MustCompile(strings.ReplaceAll(`ports are not applicable to protocol 'icmp'`, " ", `\s+`)),
			},
		},
	})
}

// The VPC's own default group is born with the VPC and refuses deletion. An
// operator who imported it needs that sentence rather than a silent success.
func TestVpcNsgResourcePlanSurfacesDefaultGroupRefusal(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "default", nsgPlanTestRuleHTTPS),
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.refuseDelete = true
					return nil
				},
			},
			{
				Config:      nsgPlanTestConfig(server.URL, "default", nsgPlanTestRuleHTTPS),
				Destroy:     true,
				ExpectError: regexp.MustCompile(strings.ReplaceAll(`HTTP 409 \(Duplicate Resource\): The VPC's default security group cannot be deleted while it is the default`, " ", `\s+`)),
			},
		},
	})
}

// A group deleted outside Terraform answers the read with a 404. Read drops it
// from state. The next plan then proposes a create rather than an update
// against a group that is gone.
func TestVpcNsgResourcePlanRecreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	config := nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.deleted = true
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A group another operator already removed answers the DELETE with a 404. The
// destroy must finish, because the group is gone either way.
func TestVpcNsgResourcePlanTreatsMissingNsgAsDeleted(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	config := nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.missingOnDelete = true
				},
				Config:  config,
				Destroy: true,
			},
		},
	})
}

// A subnet can bind to the group between the refresh and the apply. The bound
// census is a live counter, so the apply writes the fresher number.
func TestVpcNsgResourcePlanAcceptsMovedSubnetCount(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				Check:  resource.TestCheckResourceAttr(gpcnVpcNsgTest, "subnet_count", "0"),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.subnetCountOnRename = 3
				},
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-b", nsgPlanTestRuleHTTPS),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "name", "nsg-plan-b"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "subnet_count", "3"),
				),
			},
		},
	})
}

// The shared validator changes nothing until the schema carries it. The rule
// block names a summary of its own, so the guard reaches into the block too.
func TestVpcNsgResourceSchemaAttachesWhitespaceValidators(t *testing.T) {
	t.Parallel()

	var schemaResponse fwresource.SchemaResponse
	(&vpcNsgResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, &schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}

	assertWhitespaceValidator(t, schemaResponse.Schema.Attributes, "name", "Invalid security group %s")
	assertWhitespaceValidator(t, schemaResponse.Schema.Attributes, "description", "Invalid security group %s")

	ruleBlock, ok := schemaResponse.Schema.Blocks["rule"].(schema.SetNestedBlock)
	if !ok {
		t.Fatalf("rule is %T, want schema.SetNestedBlock", schemaResponse.Schema.Blocks["rule"])
	}
	assertWhitespaceValidator(t, ruleBlock.NestedObject.Attributes, "remote_cidr", "Invalid security group rule %s")
	assertWhitespaceValidator(t, ruleBlock.NestedObject.Attributes, "description", "Invalid security group rule %s")
}

// GPCN trims a name, so a configured value with outer whitespace comes back
// different and the plan never settles. The refusal arrives before the request.
func TestVpcNsgResourcePlanRefusesOuterWhitespace(t *testing.T) {
	t.Parallel()
	server, _ := startNsgPlanMockServer(t)

	whitespaceRule := `
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = "10.60.0.0/16"
    description = "ping "
  }`

	whitespaceCidrRule := `
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = " 0.0.0.0/0"
  }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      nsgPlanTestConfig(server.URL, "nsg-plan-a ", nsgPlanTestRuleHTTPS),
				ExpectError: whitespaceRefusal("Invalid security group name", "name"),
			},
			{
				Config:      nsgPlanTestConfigWithDescription(server.URL, "nsg-plan-a", " web tier", nsgPlanTestRuleHTTPS),
				ExpectError: whitespaceRefusal("Invalid security group description", "description"),
			},
			{
				Config:      nsgPlanTestConfig(server.URL, "nsg-plan-a", whitespaceRule),
				ExpectError: whitespaceRefusal("Invalid security group rule description", "description"),
			},
			{
				Config:      nsgPlanTestConfig(server.URL, "nsg-plan-a", whitespaceCidrRule),
				ExpectError: whitespaceRefusal("Invalid security group rule remote_cidr", "remote_cidr"),
			},
		},
	})
}

// GPCN stages the platform posture in the VPC's own default group as ordinary
// rule rows. The rules route replaces the whole set. An apply against that
// group must still send every rule the configuration keeps.
func TestVpcNsgResourcePlanReplacesRulesOnDefaultGroup(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	lastRulesPutCount := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			state.mu.Lock()
			defer state.mu.Unlock()
			sent, ok := state.lastRulesPut["rules"].([]any)
			if !ok {
				return fmt.Errorf("expected a rules array in the body, got %T", state.lastRulesPut["rules"])
			}
			if len(sent) != want {
				return fmt.Errorf("expected the complete set of %d rules, got %d", want, len(sent))
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "default", nsgPlanTestRuleHTTPS),
				Check:  resource.TestCheckResourceAttr(gpcnVpcNsgTest, "is_default", "false"),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.isDefault = true
				},
				Config: nsgPlanTestConfig(server.URL, "default", nsgPlanTestRuleHTTPS+nsgPlanTestRulePing),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "is_default", "true"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					lastRulesPutCount(2),
				),
			},
		},
	})
}

// Terraform reconciles a description in place, so Read must show the one GPCN
// holds. A refresh that kept the stale value would plan nothing and leave the
// drift in place.
func TestVpcNsgResourcePlanRefreshesDescription(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	config := nsgPlanTestConfigWithDescription(server.URL, "nsg-plan-a", "web tier", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr(gpcnVpcNsgTest, "description", "web tier"),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.description = "changed out of band"
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcNsgTest, "description", "web tier"),
			},
		},
	})
}

// The plan harness carries no warning assertion, so ModifyPlan is driven
// directly. The warning tells the operator what a replace costs while the plan
// can still be refused.
func TestVpcNsgModifyPlanWarnsBeforeReplacingDefaultRulesUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	groupResource := &vpcNsgResource{}

	var schemaResponse fwresource.SchemaResponse
	groupResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

	ruleSet := func(remoteCidr string) types.Set {
		set, diags := types.SetValueFrom(ctx, vpcnsgs.RuleObjectType(), []vpcnsgs.RuleModel{{
			Direction:    types.StringValue("ingress"),
			Protocol:     types.StringValue("tcp"),
			PortRangeMin: types.Int64Value(443),
			PortRangeMax: types.Int64Value(443),
			RemoteCidr:   types.StringValue(remoteCidr),
			Description:  types.StringNull(),
		}})
		if diags.HasError() {
			t.Fatalf("failed to build the rule set: %v", diags)
		}
		return set
	}

	model := func(isDefault bool, rules types.Set) vpcnsgs.ResourceModel {
		return vpcnsgs.ResourceModel{
			ID:            types.StringValue(nsgPlanTestID),
			VpcID:         types.StringValue(nsgPlanTestVpcID),
			Name:          types.StringValue("nsg-plan-a"),
			Description:   types.StringValue(""),
			Rules:         rules,
			IsDefault:     types.BoolValue(isDefault),
			State:         types.StringValue("ready"),
			FailureReason: types.StringNull(),
			RuleCount:     types.Int64Value(1),
			SubnetCount:   types.Int64Value(0),
			CreatedTime:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
			LastUpdated:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
		}
	}

	modifyPlan := func(isDefault bool, stateRules, planRules types.Set) fwresource.ModifyPlanResponse {
		t.Helper()

		priorState := tfsdk.State{Schema: schemaResponse.Schema}
		diags := priorState.Set(ctx, model(isDefault, stateRules))
		if diags.HasError() {
			t.Fatalf("failed to build the prior state: %v", diags)
		}

		plan := tfsdk.Plan{Schema: schemaResponse.Schema}
		diags = plan.Set(ctx, model(isDefault, planRules))
		if diags.HasError() {
			t.Fatalf("failed to build the plan: %v", diags)
		}

		response := fwresource.ModifyPlanResponse{Plan: plan}
		groupResource.ModifyPlan(ctx, fwresource.ModifyPlanRequest{State: priorState, Plan: plan}, &response)
		if response.Diagnostics.HasError() {
			t.Fatalf("ModifyPlan reported errors: %v", response.Diagnostics.Errors())
		}
		return response
	}

	changed := modifyPlan(true, ruleSet("0.0.0.0/0"), ruleSet("10.60.0.0/16"))
	warnings := changed.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != "Replacing the rules of the VPC default security group" {
		t.Errorf("summary = %q, want %q", got, "Replacing the rules of the VPC default security group")
	}
	wantDetail := fmt.Sprintf(vpcnsgs.WarnDetailDefaultNsgRulesReplaced, nsgPlanTestID)
	if got := warnings[0].Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}

	unchanged := modifyPlan(true, ruleSet("0.0.0.0/0"), ruleSet("0.0.0.0/0"))
	if got := unchanged.Diagnostics.WarningsCount(); got != 0 {
		t.Errorf("warnings for an unchanged default group = %d, want 0", got)
	}

	ordinary := modifyPlan(false, ruleSet("0.0.0.0/0"), ruleSet("10.60.0.0/16"))
	if got := ordinary.Diagnostics.WarningsCount(); got != 0 {
		t.Errorf("warnings for an ordinary group = %d, want 0", got)
	}
}

// GPCN inserts the group row before it dispatches the build job. The row then
// holds the name until someone deletes it. A failed job must therefore leave
// the id in state. The destroy then deletes the group instead of leaving the
// next apply to collide with it.
func TestVpcNsgResourcePlanKeepsTheIdWhenTheCreateJobFails(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)
	state.failTheCreateJob()

	config := nsgPlanTestConfig(server.URL, "build-failed", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpNsgCreatedButJobFailed,
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkNsgDestroyDeletedTheGroup(state),
			},
		},
	})
}

// checkNsgDestroyDeletedTheGroup proves the failed create wrote the id to
// state. The mock routes the delete by that id. A create that keeps the id to
// itself leaves the destroy nothing to delete.
func checkNsgDestroyDeletedTheGroup(state *nsgPlanTestServerState) func(*terraform.State) error {
	return func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if !state.deleted {
			return fmt.Errorf("expected the destroy to delete the security group, but the mock still serves it")
		}
		return nil
	}
}

// A build GPCN gave up on still reads back cleanly. The operator learns of it
// through the state and the reason the API files against the row.
func TestVpcNsgResourcePlanReadsFailedGroup(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	config := nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "state", "ready"),
					resource.TestCheckNoResourceAttr(gpcnVpcNsgTest, "failure_reason"),
				),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.rowState = "failed"
					state.failureReason = nsgPlanTestFailedReason
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "state", "failed"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "failure_reason", nsgPlanTestFailedReason),
				),
			},
		},
	})
}

// The plan harness carries no warning assertion, so Read is driven directly. A
// test on the constructor alone leaves the call site unguarded.
func TestVpcNsgReadWarnsOnFailedGroupUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	readGroup := func(group *nsgPlanTestServerState) fwresource.ReadResponse {
		t.Helper()

		_, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
			T: t,
			Handler: func(w http.ResponseWriter, _ *http.Request) {
				testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": group.detail()})
			},
		})

		groupResource := &vpcNsgResource{client: gpcnClient}

		var schemaResponse fwresource.SchemaResponse
		groupResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

		rules, diags := types.SetValueFrom(ctx, vpcnsgs.RuleObjectType(), []vpcnsgs.RuleModel{})
		if diags.HasError() {
			t.Fatalf("failed to build the rule set: %v", diags)
		}

		priorState := tfsdk.State{Schema: schemaResponse.Schema}
		diags = priorState.Set(ctx, vpcnsgs.ResourceModel{
			ID:            types.StringValue(nsgPlanTestID),
			VpcID:         types.StringValue(nsgPlanTestVpcID),
			Name:          types.StringValue("nsg-plan-a"),
			Description:   types.StringValue(""),
			Rules:         rules,
			IsDefault:     types.BoolValue(false),
			State:         types.StringValue("ready"),
			FailureReason: types.StringNull(),
			RuleCount:     types.Int64Value(0),
			SubnetCount:   types.Int64Value(0),
			CreatedTime:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
			LastUpdated:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
		})
		if diags.HasError() {
			t.Fatalf("failed to build the prior state: %v", diags)
		}

		readResponse := fwresource.ReadResponse{
			State: tfsdk.State{Schema: schemaResponse.Schema, Raw: priorState.Raw},
		}
		groupResource.Read(ctx, fwresource.ReadRequest{State: priorState}, &readResponse)

		if readResponse.Diagnostics.HasError() {
			t.Fatalf("Read reported errors: %v", readResponse.Diagnostics.Errors())
		}
		return readResponse
	}

	readInState := func(rowState, failureReason string) fwresource.ReadResponse {
		t.Helper()

		return readGroup(&nsgPlanTestServerState{
			name:          "nsg-plan-a",
			rules:         []map[string]any{},
			rowState:      rowState,
			failureReason: failureReason,
		})
	}

	failed := readInState("failed", nsgPlanTestFailedReason)
	warnings := failed.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != "Security group is in the failed state" {
		t.Errorf("summary = %q, want %q", got, "Security group is in the failed state")
	}
	wantDetail := "Security group 66666666-6666-4666-8666-666666666666 is in the failed state: the provider rejected the group. Apply its rules again or delete the group and create it again."
	if got := warnings[0].Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}

	// GPCN can park a group with no reason recorded.
	noReason := readInState("failed", "")
	noReasonWarnings := noReason.Diagnostics.Warnings()
	if len(noReasonWarnings) != 1 {
		t.Fatalf("warnings with no reason = %v, want exactly one", noReasonWarnings)
	}
	wantNoReasonDetail := "Security group 66666666-6666-4666-8666-666666666666 is in the failed state. Apply its rules again or delete the group and create it again."
	if got := noReasonWarnings[0].Detail(); got != wantNoReasonDetail {
		t.Errorf("detail with no reason = %q, want %q", got, wantNoReasonDetail)
	}

	// A reason that arrives as "" takes the same warning. Nothing else in the
	// suite sends that shape, so the guard's second half rests on this arm.
	emptyReason := readGroup(&nsgPlanTestServerState{
		name:               "nsg-plan-a",
		rules:              []map[string]any{},
		rowState:           "failed",
		emptyFailureReason: true,
	})
	emptyReasonWarnings := emptyReason.Diagnostics.Warnings()
	if len(emptyReasonWarnings) != 1 {
		t.Fatalf("warnings with an empty reason = %v, want exactly one", emptyReasonWarnings)
	}
	if got := emptyReasonWarnings[0].Detail(); got != wantNoReasonDetail {
		t.Errorf("detail with an empty reason = %q, want %q", got, wantNoReasonDetail)
	}

	ready := readInState("ready", "")
	if got := ready.Diagnostics.WarningsCount(); got != 0 {
		t.Errorf("warnings for a ready group = %d, want 0", got)
	}
}

// GPCN answers the read-back after a build that worked. A failure there leaves
// the group and the id in state. The error therefore carries the tainted
// remedy, not a bare HTTP failure.
func TestVpcNsgResourcePlanKeepsTheIdWhenTheReadBackFails(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)
	state.failTheNextRead()

	config := nsgPlanTestConfigWithoutRetries(server.URL, "read-back-failed", nsgPlanTestRuleHTTPS)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpNsgCreatedButReadBackFailed,
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkNsgDestroyDeletedTheGroup(state),
			},
		},
	})
}

const (
	nsgPlanTestVpnConnectionID = "77777777-7777-4777-8777-777777777777"
	nsgPlanTestVpnCidr         = "192.168.20.0/24"
	nsgPlanTestVpnDescription  = "VPN connection office (77777777-7777-4777-8777-777777777777)"
	nsgPlanTestVpnDeclaredRule = `
  rule {
    direction   = "ingress"
    protocol    = "all"
    remote_cidr = "192.168.20.0/24"
    description = "mine now"
  }`
)

// seedVpnRule files the VPN connection's rule after the group exists, as the
// connection's job does.
func seedVpnRule(state *nsgPlanTestServerState) resource.TestCheckFunc {
	return func(*terraform.State) error {
		state.addVpnRule(nsgPlanTestVpnConnectionID, nsgPlanTestVpnCidr, nsgPlanTestVpnDescription)
		return nil
	}
}

func nsgPlanTestLastRulesPut(state *nsgPlanTestServerState) ([]any, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.lastRulesPut == nil {
		return nil, fmt.Errorf("expected a rules PUT, got none")
	}
	sent, ok := state.lastRulesPut["rules"].([]any)
	if !ok {
		return nil, fmt.Errorf("expected a rules array in the body, got %T", state.lastRulesPut["rules"])
	}
	return sent, nil
}

// A rule a VPN connection owns is not the operator's to manage. State holds
// the customer rule only, and the plan against that configuration is empty.
func TestVpcNsgResourceVpnRulesHiddenFromState(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				Check:  seedVpnRule(state),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.0.protocol", "tcp"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule_count", "2"),
				),
			},
			{
				ResourceName:      gpcnVpcNsgTest,
				ImportState:       true,
				ImportStateId:     nsgPlanTestVpcID + "/" + nsgPlanTestID,
				ImportStateVerify: true,
			},
		},
	})
}

// The rules PUT is a full replace, so it must carry the VPN rule back or the
// API tombstones it for good.
func TestVpcNsgResourceUpdateResendsVpnRules(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				Check:  seedVpnRule(state),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+nsgPlanTestRulePing),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					func(*terraform.State) error {
						sent, err := nsgPlanTestLastRulesPut(state)
						if err != nil {
							return err
						}
						want := map[string]any{
							"direction":   "ingress",
							"protocol":    "all",
							"remoteCidr":  nsgPlanTestVpnCidr,
							"description": nsgPlanTestVpnDescription,
						}
						if len(sent) != 3 {
							return fmt.Errorf("expected 3 rules in the body, got %v", sent)
						}
						if !reflect.DeepEqual(sent[2], want) {
							return fmt.Errorf("expected the owned rule %v last, got %v", want, sent[2])
						}
						return nil
					},
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+nsgPlanTestRulePing),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// A configured rule with the VPN rule's content takes it over. The body then
// carries the configured rule once, with the configured description.
func TestVpcNsgResourceVpnRuleTakenOver(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				Check:  seedVpnRule(state),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+nsgPlanTestVpnDeclaredRule),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					func(*terraform.State) error {
						sent, err := nsgPlanTestLastRulesPut(state)
						if err != nil {
							return err
						}
						if len(sent) != 2 {
							return fmt.Errorf("expected 2 rules in the body, got %v", sent)
						}
						for _, entry := range sent {
							rule, _ := entry.(map[string]any)
							if rule["remoteCidr"] == nsgPlanTestVpnCidr && rule["description"] != "mine now" {
								return fmt.Errorf("expected the configured description, got %v", rule)
							}
							if rule["description"] == nsgPlanTestVpnDescription {
								return fmt.Errorf("the owned rule was re-sent beside the configured one: %v", sent)
							}
						}
						return nil
					},
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+nsgPlanTestVpnDeclaredRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

var (
	nsgPlanTestHTTPSRule = vpcnsgs.RuleModel{
		Direction:    types.StringValue("ingress"),
		Protocol:     types.StringValue("tcp"),
		PortRangeMin: types.Int64Value(443),
		PortRangeMax: types.Int64Value(443),
		RemoteCidr:   types.StringValue("0.0.0.0/0"),
		Description:  types.StringValue("https"),
	}
	nsgPlanTestPingRule = vpcnsgs.RuleModel{
		Direction:    types.StringValue("ingress"),
		Protocol:     types.StringValue("icmp"),
		PortRangeMin: types.Int64Null(),
		PortRangeMax: types.Int64Null(),
		RemoteCidr:   types.StringValue("10.60.0.0/16"),
		Description:  types.StringNull(),
	}
)

// seedNsgHTTPSRule files the one customer rule the direct Update tests start
// from.
func seedNsgHTTPSRule(state *nsgPlanTestServerState) {
	state.name = "nsg-plan-a"
	state.storeRules(map[string]any{"rules": []any{map[string]any{
		"direction": "ingress", "protocol": "tcp", "portRangeMin": float64(443), "portRangeMax": float64(443),
		"remoteCidr": "0.0.0.0/0", "description": "https",
	}}})
}

// updateNsgDirectly calls Update with a prior state holding the https rule and
// a plan holding the planned rules.
func updateNsgDirectly(t *testing.T, server *httptest.Server, planned ...vpcnsgs.RuleModel) fwresource.UpdateResponse {
	t.Helper()

	ctx := context.Background()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: server.Config.Handler.ServeHTTP})
	groupResource := &vpcNsgResource{client: gpcnClient}

	var schemaResponse fwresource.SchemaResponse
	groupResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

	model := func(rules ...vpcnsgs.RuleModel) vpcnsgs.ResourceModel {
		set, diags := types.SetValueFrom(ctx, vpcnsgs.RuleObjectType(), rules)
		if diags.HasError() {
			t.Fatalf("failed to build the rule set: %v", diags)
		}
		return vpcnsgs.ResourceModel{
			ID:            types.StringValue(nsgPlanTestID),
			VpcID:         types.StringValue(nsgPlanTestVpcID),
			Name:          types.StringValue("nsg-plan-a"),
			Description:   types.StringValue(""),
			Rules:         set,
			IsDefault:     types.BoolValue(false),
			State:         types.StringValue("ready"),
			FailureReason: types.StringNull(),
			RuleCount:     types.Int64Value(2),
			SubnetCount:   types.Int64Value(0),
			CreatedTime:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
			LastUpdated:   types.StringValue("Friday, 02-Jan-26 15:04:05 UTC"),
		}
	}

	priorState := tfsdk.State{Schema: schemaResponse.Schema}
	if diags := priorState.Set(ctx, model(nsgPlanTestHTTPSRule)); diags.HasError() {
		t.Fatalf("failed to build the prior state: %v", diags)
	}
	plan := tfsdk.Plan{Schema: schemaResponse.Schema}
	if diags := plan.Set(ctx, model(planned...)); diags.HasError() {
		t.Fatalf("failed to build the plan: %v", diags)
	}

	response := fwresource.UpdateResponse{State: tfsdk.State{Schema: schemaResponse.Schema, Raw: priorState.Raw}}
	groupResource.Update(ctx, fwresource.UpdateRequest{State: priorState, Plan: plan}, &response)
	return response
}

// A rename of the connection can race the replace and relabel the rule to the
// customer. The apply still succeeds and warns, because the rule now outlives
// the connection.
func TestVpcNsgResourceVpnRuleOwnershipLostWarns(t *testing.T) {
	t.Parallel()

	server, state := startNsgPlanMockServer(t)
	state.loseVpnOwnership = true
	seedNsgHTTPSRule(state)
	state.addVpnRule(nsgPlanTestVpnConnectionID, nsgPlanTestVpnCidr, nsgPlanTestVpnDescription)

	response := updateNsgDirectly(t, server, nsgPlanTestHTTPSRule, nsgPlanTestPingRule)
	if response.Diagnostics.HasError() {
		t.Fatalf("Update reported errors: %v", response.Diagnostics.Errors())
	}

	warnings := response.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != "GPCN™ VPN connection rule changed owner" {
		t.Errorf("summary = %q", got)
	}
	wantDetail := "Rule for 192.168.20.0/24 is no longer managed by VPN connection " + nsgPlanTestVpnConnectionID + ", so it's now part of this security group's rule set. Add it to this configuration to keep it; otherwise the next apply removes it."
	if got := warnings[0].Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}
}

// The ownership read comes after the replace has succeeded. A failure of that
// read warns and never fails the apply.
func TestVpcNsgResourceVpnRuleCheckFailureWarns(t *testing.T) {
	t.Parallel()

	server, state := startNsgPlanMockServer(t)
	seedNsgHTTPSRule(state)
	state.addVpnRule(nsgPlanTestVpnConnectionID, nsgPlanTestVpnCidr, nsgPlanTestVpnDescription)
	state.failReadAt = 2

	response := updateNsgDirectly(t, server, nsgPlanTestHTTPSRule, nsgPlanTestPingRule)
	if response.Diagnostics.HasError() {
		t.Fatalf("Update reported errors: %v", response.Diagnostics.Errors())
	}

	warnings := response.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != "GPCN™ VPN connection rule check skipped" {
		t.Errorf("summary = %q", got)
	}
	if got := warnings[0].Detail(); !strings.Contains(got, "Internal server error") {
		t.Errorf("detail = %q, want the API message", got)
	}
}

// The API refuses more than 100 rules. The provider refuses first, with the
// split between the configuration and the connections, and sends no PUT.
func TestVpcNsgResourceVpnRulesOverLimitRefused(t *testing.T) {
	t.Parallel()

	server, state := startNsgPlanMockServer(t)
	seedNsgHTTPSRule(state)
	for index := range 99 {
		state.addVpnRule(nsgPlanTestVpnConnectionID, fmt.Sprintf("192.168.%d.0/24", index), nsgPlanTestVpnDescription)
	}
	state.mu.Lock()
	puts := state.rulesPuts
	state.mu.Unlock()

	response := updateNsgDirectly(t, server, nsgPlanTestHTTPSRule, nsgPlanTestPingRule)
	errs := response.Diagnostics.Errors()
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly one", errs)
	}
	want := "This security group would hold 101 rules: 2 from this configuration and 99 managed by VPN connections. A security group holds at most 100."
	if got := errs[0].Detail(); got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.rulesPuts != puts {
		t.Errorf("a rules PUT went out despite the refusal")
	}
}

// A configured rule that copies an owned rule exactly, description included,
// stays the connection's. State keeps it, so the next plan is empty.
func TestVpcNsgResourceVpnRuleDeclaredUnchangedIsStable(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	exact := fmt.Sprintf(`
  rule {
    direction   = "ingress"
    protocol    = "all"
    remote_cidr = %q
    description = %q
  }`, nsgPlanTestVpnCidr, nsgPlanTestVpnDescription)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
				Check:  seedVpnRule(state),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+exact),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					func(*terraform.State) error {
						sent, err := nsgPlanTestLastRulesPut(state)
						if err != nil {
							return err
						}
						if len(sent) != 2 {
							return fmt.Errorf("expected 2 rules in the body, got %v", sent)
						}
						return nil
					},
				),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+exact),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// A response with no vpnConnectionId key is the API before the field shipped.
// The PUT body is then the configuration and nothing else.
func TestVpcNsgResourceNoOwnerFieldUnchanged(t *testing.T) {
	t.Parallel()
	server, state := startNsgPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS),
			},
			{
				Config: nsgPlanTestConfig(server.URL, "nsg-plan-a", nsgPlanTestRuleHTTPS+nsgPlanTestRulePing),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					func(*terraform.State) error {
						sent, err := nsgPlanTestLastRulesPut(state)
						if err != nil {
							return err
						}
						want := []any{
							map[string]any{"direction": "ingress", "protocol": "tcp", "portRangeMin": float64(443), "portRangeMax": float64(443), "remoteCidr": "0.0.0.0/0", "description": "https"},
							map[string]any{"direction": "ingress", "protocol": "icmp", "remoteCidr": "10.60.0.0/16"},
						}
						// The set orders its elements, so compare without order.
						if len(sent) != len(want) {
							return fmt.Errorf("expected %d rules, got %v", len(want), sent)
						}
						for _, expected := range want {
							found := false
							for _, entry := range sent {
								if reflect.DeepEqual(entry, expected) {
									found = true
								}
							}
							if !found {
								return fmt.Errorf("expected %v in the body, got %v", expected, sent)
							}
						}
						return nil
					},
				),
			},
		},
	})
}
