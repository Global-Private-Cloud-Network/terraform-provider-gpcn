package vpcnsgs

import (
	"context"
	"net/http"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	unitTestVpcID     = "vpc-1"
	unitTestNsgID     = "nsg-1"
	unitTestTimestamp = "2026-01-02T15:04:05Z"
)

func unitTestRFC850(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("failed to parse the fixture timestamp %q: %v", raw, err)
	}
	return parsed.Format(time.RFC850)
}

func unitTestDetail() *NsgDetail {
	return &NsgDetail{
		Nsg: ApiNsg{
			ID:            unitTestNsgID,
			Name:          "nsg-a",
			Description:   nil,
			IsDefault:     false,
			State:         "ready",
			FailureReason: nil,
			RuleCount:     1,
			SubnetCount:   2,
			CreatedAt:     unitTestTimestamp,
			UpdatedAt:     unitTestTimestamp,
		},
		Rules: []ApiRule{unitTestIcmpRule()},
	}
}

func unitTestIcmpRule() ApiRule {
	return ApiRule{
		ID:           "rule-1",
		Direction:    "ingress",
		Protocol:     "icmp",
		PortRangeMin: nil,
		PortRangeMax: nil,
		RemoteCidr:   "0.0.0.0/0",
		Description:  nil,
	}
}

func unitTestTCPRule() ApiRule {
	min := int64(443)
	max := int64(443)
	description := "https"
	return ApiRule{
		ID:           "rule-2",
		Direction:    "ingress",
		Protocol:     "tcp",
		PortRangeMin: &min,
		PortRangeMax: &max,
		RemoteCidr:   "203.0.113.0/24",
		Description:  &description,
	}
}

// A null description reaches Terraform as the empty string the schema defaults
// to. The counts and the default flag come straight off the group.
func TestMapNsgResponseToModelFillsComputedUnit(t *testing.T) {
	t.Parallel()

	model, diags := MapNsgResponseToModel(context.Background(), unitTestDetail(), ResourceModel{})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	if got := model.ID.ValueString(); got != unitTestNsgID {
		t.Errorf("expected id %q, got %q", unitTestNsgID, got)
	}
	if got := model.Description.ValueString(); got != "" {
		t.Errorf("expected an empty description, got %q", got)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("expected a null failure_reason, got %q", model.FailureReason.ValueString())
	}
	if model.IsDefault.ValueBool() {
		t.Error("expected is_default false")
	}
	if got := model.RuleCount.ValueInt64(); got != 1 {
		t.Errorf("expected rule_count 1, got %d", got)
	}
	if got := model.SubnetCount.ValueInt64(); got != 2 {
		t.Errorf("expected subnet_count 2, got %d", got)
	}
	want := unitTestRFC850(t, unitTestTimestamp)
	if got := model.CreatedTime.ValueString(); got != want {
		t.Errorf("expected created_time %q, got %q", want, got)
	}
}

// A rule with no ports and no description keeps those attributes null. Null
// and absent mean the same thing to the API.
func TestRulesToSetMapsNullPortsUnit(t *testing.T) {
	t.Parallel()

	set, diags := RulesToSet(context.Background(), []ApiRule{unitTestIcmpRule()})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	rules, diags := RulesFromSet(context.Background(), set)
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if len(rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(rules))
	}
	if !rules[0].PortRangeMin.IsNull() || !rules[0].PortRangeMax.IsNull() {
		t.Errorf("expected null ports, got %v and %v", rules[0].PortRangeMin, rules[0].PortRangeMax)
	}
	if !rules[0].Description.IsNull() {
		t.Errorf("expected a null rule description, got %q", rules[0].Description.ValueString())
	}
	if got := rules[0].RemoteCidr.ValueString(); got != "0.0.0.0/0" {
		t.Errorf("expected remote_cidr %q, got %q", "0.0.0.0/0", got)
	}
}

// The rules are exactly what this resource manages, so Read replaces the whole
// set rather than filling it when absent.
func TestRefreshNsgModelFromResponseReplacesRuleSetUnit(t *testing.T) {
	t.Parallel()

	stale, diags := RulesToSet(context.Background(), []ApiRule{unitTestIcmpRule()})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	detail := unitTestDetail()
	detail.Nsg.Name = "renamed-out-of-band"
	detail.Rules = []ApiRule{unitTestIcmpRule(), unitTestTCPRule()}

	model, diags := RefreshNsgModelFromResponse(context.Background(), detail, ResourceModel{
		Name:  types.StringValue("nsg-a"),
		Rules: stale,
	})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	if got := model.Name.ValueString(); got != "renamed-out-of-band" {
		t.Errorf("expected the refreshed name, got %q", got)
	}
	if got := len(model.Rules.Elements()); got != 2 {
		t.Errorf("expected the refreshed rule set to hold two rules, got %d", got)
	}
}

// A null port or description must not reach the wire as a key. The API reads
// an absent key and a null one the same way. The body is also strict.
func TestRuleRequestBodiesOmitNullKeysUnit(t *testing.T) {
	t.Parallel()

	set, diags := RulesToSet(context.Background(), []ApiRule{unitTestIcmpRule(), unitTestTCPRule()})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	rules, diags := RulesFromSet(context.Background(), set)
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	bodies := RuleRequestBodies(rules)
	if len(bodies) != 2 {
		t.Fatalf("expected two rule bodies, got %d", len(bodies))
	}

	for _, body := range bodies {
		if _, present := body["id"]; present {
			t.Error("expected no id key: the API ignores it and rule identity is content")
		}
		if body["protocol"] == "icmp" {
			if _, present := body["portRangeMin"]; present {
				t.Error("expected no portRangeMin key on an icmp rule")
			}
			if _, present := body["description"]; present {
				t.Error("expected no description key when the rule has none")
			}
		}
		if body["protocol"] == "tcp" {
			if got, _ := body["portRangeMin"].(int64); got != 443 {
				t.Errorf("expected portRangeMin 443, got %v", body["portRangeMin"])
			}
			if got, _ := body["description"].(string); got != "https" {
				t.Errorf("expected the rule description, got %v", body["description"])
			}
		}
	}
}

// The rules PUT is a full replace keyed on content. The body must carry every
// rule the configuration still wants. It does not carry only the new ones.
func TestReplaceNsgRulesSendsCompleteSetUnit(t *testing.T) {
	t.Parallel()

	var sentBody map[string]any
	var sentPath string
	server, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				sentPath = r.URL.Path
				sentBody = testutil.ReadRequestBody(r)
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": "job-rules"},
				})
				return
			}
			testutil.HandleJobResponse(w, "job-rules", unitTestNsgID, true)
		},
	})
	defer server.Close()

	set, diags := RulesToSet(context.Background(), []ApiRule{unitTestIcmpRule(), unitTestTCPRule()})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	rules, diags := RulesFromSet(context.Background(), set)
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	if err := ReplaceNsgRules(gpcnClient, context.Background(), unitTestVpcID, unitTestNsgID, rules); err != nil {
		t.Fatalf("expected the rules replace to succeed, got %v", err)
	}

	wantPath := BaseURLV1 + unitTestVpcID + "/nsgs/" + unitTestNsgID + "/rules"
	if sentPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, sentPath)
	}
	sent, ok := sentBody["rules"].([]any)
	if !ok {
		t.Fatalf("expected a rules array in the body, got %T", sentBody["rules"])
	}
	if len(sent) != 2 {
		t.Errorf("expected the complete set of two rules, got %d", len(sent))
	}
}

func unitTestRuleObject(direction, protocol, remoteCidr string, min, max types.Int64) types.Object {
	return types.ObjectValueMust(RuleObjectType().AttrTypes, map[string]attr.Value{
		"direction":      types.StringValue(direction),
		"protocol":       types.StringValue(protocol),
		"port_range_min": min,
		"port_range_max": max,
		"remote_cidr":    types.StringValue(remoteCidr),
		"description":    types.StringNull(),
	})
}

// Each refusal quotes the API's own sentence. The plan gives the operator the
// same words the apply would have produced.
func TestRulePortValidatorMessagesUnit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		rule  types.Object
		want  string
		valid bool
	}{
		{
			name:  "ports on icmp",
			rule:  unitTestRuleObject("ingress", "icmp", "0.0.0.0/0", types.Int64Value(1), types.Int64Value(2)),
			want:  "ports are not applicable to protocol 'icmp'",
			valid: false,
		},
		{
			name:  "ports on all",
			rule:  unitTestRuleObject("egress", "all", "0.0.0.0/0", types.Int64Value(80), types.Int64Null()),
			want:  "ports are not applicable to protocol 'all'",
			valid: false,
		},
		{
			name:  "only a minimum",
			rule:  unitTestRuleObject("ingress", "tcp", "0.0.0.0/0", types.Int64Value(80), types.Int64Null()),
			want:  "portRangeMin and portRangeMax must be provided together",
			valid: false,
		},
		{
			name:  "reversed range",
			rule:  unitTestRuleObject("ingress", "udp", "0.0.0.0/0", types.Int64Value(90), types.Int64Value(80)),
			want:  "portRangeMin must not exceed portRangeMax",
			valid: false,
		},
		{
			name:  "a valid range",
			rule:  unitTestRuleObject("ingress", "tcp", "0.0.0.0/0", types.Int64Value(80), types.Int64Value(90)),
			valid: true,
		},
		{
			name:  "no ports at all",
			rule:  unitTestRuleObject("ingress", "icmp", "0.0.0.0/0", types.Int64Null(), types.Int64Null()),
			valid: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := validator.ObjectRequest{ConfigValue: testCase.rule}
			response := &validator.ObjectResponse{}
			RulePortValidator{}.ValidateObject(context.Background(), request, response)

			if testCase.valid {
				if response.Diagnostics.HasError() {
					t.Fatalf("expected no error, got %v", response.Diagnostics)
				}
				return
			}
			if !response.Diagnostics.HasError() {
				t.Fatalf("expected the error %q, got none", testCase.want)
			}
			if got := response.Diagnostics.Errors()[0].Detail(); got != testCase.want {
				t.Errorf("expected the detail %q, got %q", testCase.want, got)
			}
		})
	}
}

// The rules PUT is a full replace, and the backend puts no guard on the default
// group. GPCN removes only the rows the desired set leaves out. The sentence
// must not promise a deletion the configuration prevents.
func TestDefaultNsgRulesWarningUnit(t *testing.T) {
	t.Parallel()

	diags := DefaultNsgRulesWarning(true, unitTestNsgID)

	if got := diags.WarningsCount(); got != 1 {
		t.Fatalf("expected one warning, got %d", got)
	}
	warning := diags.Warnings()[0]
	if got := warning.Summary(); got != "Replacing the rules of the VPC default security group" {
		t.Errorf("expected the ruled summary, got %q", got)
	}
	want := "Security group 'nsg-1' is the VPC's own default group. GPCN replaces the whole rule set on every change, so this apply deletes any rule this configuration does not list, including the two GPCN staged with the VPC: 'Default: allow all outbound traffic' and 'Default: allow traffic from this VPC'. Add them to the configuration to keep them."
	if got := warning.Detail(); got != want {
		t.Errorf("expected the detail %q, got %q", want, got)
	}
}

// An ordinary group holds no platform posture, so a replace there is silent.
func TestDefaultNsgRulesWarningSilentForOrdinaryGroupUnit(t *testing.T) {
	t.Parallel()

	if got := DefaultNsgRulesWarning(false, unitTestNsgID).WarningsCount(); got != 0 {
		t.Errorf("expected no warning for an ordinary group, got %d", got)
	}
}

// Terraform reconciles a description in place, so Read shows the one GPCN
// holds. A null description reads as the empty string the schema defaults to.
func TestRefreshNsgModelFromResponseUpdatesDescriptionUnit(t *testing.T) {
	t.Parallel()

	changed := "changed out of band"
	detail := unitTestDetail()
	detail.Nsg.Description = &changed

	model, diags := RefreshNsgModelFromResponse(context.Background(), detail, ResourceModel{
		Description: types.StringValue("web tier"),
	})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if got := model.Description.ValueString(); got != changed {
		t.Errorf("expected the refreshed description %q, got %q", changed, got)
	}

	detail.Nsg.Description = nil
	model, diags = RefreshNsgModelFromResponse(context.Background(), detail, ResourceModel{
		Description: types.StringValue("web tier"),
	})
	if diags.HasError() {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if got := model.Description.ValueString(); got != "" {
		t.Errorf("expected a cleared description to read as the empty string, got %q", got)
	}
}

// One create carries one correlation id. A poll that mints its own leaves the
// support log with no link between the create call and the job it waits for.
func TestCreateNsgCarriesOneCorrelationIDUnit(t *testing.T) {
	t.Parallel()

	var createID, pollID string
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == client.JOBS_BASE_URL_V1 {
				if pollID == "" {
					pollID = r.Header.Get("x-Correlation-ID")
				}
				testutil.HandleJobResponse(w, "job-create", unitTestNsgID, true)
				return
			}
			createID = r.Header.Get("x-Correlation-ID")
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"nsgId": unitTestNsgID, "jobId": "job-create"},
			})
		},
	})

	ctx := client.WithCorrelationID(context.Background())
	issued, err := IssueCreateNsg(gpcnClient, ctx, unitTestVpcID, "nsg-a", "", nil)
	if err != nil {
		t.Fatalf("expected the create to be issued, got %v", err)
	}
	if err := PollCreateNsg(gpcnClient, ctx, issued.JobID); err != nil {
		t.Fatalf("expected the build poll to succeed, got %v", err)
	}

	if createID == "" {
		t.Fatal("the create carried no correlation id")
	}
	if pollID != createID {
		t.Errorf("poll correlation id = %q, want the create's %q", pollID, createID)
	}
}
