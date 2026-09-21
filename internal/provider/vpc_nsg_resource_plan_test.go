package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/vpcnsgs"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	nsgPlanTestVpcID     = "55555555-5555-4555-8555-555555555555"
	nsgPlanTestID        = "66666666-6666-4666-8666-666666666666"
	nsgPlanTestTimestamp = "2026-01-02T15:04:05Z"
	gpcnVpcNsgTest       = "gpcn_vpc_nsg.test"
)

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
}

func (s *nsgPlanTestServerState) storeRules(body map[string]any) {
	sent, _ := body["rules"].([]any)
	stored := make([]map[string]any, 0, len(sent))
	for index, entry := range sent {
		rule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		rule["id"] = fmt.Sprintf("rule-%d", index)
		stored = append(stored, rule)
	}
	s.rules = stored
}

func (s *nsgPlanTestServerState) detail() map[string]any {
	var description any
	if s.description != "" {
		description = s.description
	}
	return map[string]any{
		"nsg": map[string]any{
			"id":            nsgPlanTestID,
			"name":          s.name,
			"description":   description,
			"isDefault":     s.isDefault,
			"state":         "ready",
			"failureReason": nil,
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

	state := &nsgPlanTestServerState{rules: []map[string]any{}}

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
			deleted := state.deleted
			detail := state.detail()
			state.mu.Unlock()
			if deleted {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"Security group not found","error":{"code":"RESOURCE_NOT_FOUND","statusCode":404,"details":null}}`)
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
				fmt.Fprint(w, `{"success":false,"message":"Security group not found","error":{"code":"RESOURCE_NOT_FOUND","statusCode":404,"details":null}}`)
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
				fmt.Fprint(w, `{"success":false,"message":"The VPC's default security group cannot be deleted while it is the default","error":{"code":"DUPLICATE_RESOURCE","statusCode":409,"details":null}}`)
				return
			}
			testutil.HandleCreateJobResponse(w, "job-delete", "Operation initiated successfully")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", nsgPlanTestID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

func nsgPlanTestConfig(host, name, rules string) string {
	return nsgPlanTestConfigWithDescription(host, name, "", rules)
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
				ExpectError: regexp.MustCompile(strings.ReplaceAll(`default security group cannot be deleted while it is the default`, " ", `\s+`)),
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
