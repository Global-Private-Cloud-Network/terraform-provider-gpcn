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
	"terraform-provider-gpcn/internal/vpcsubnets"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// regexpSubnetCreatedButReadBackFailed pins the R155 sentence for a row the
// platform carved and then would not serve back.
var regexpSubnetCreatedButReadBackFailed = regexp.MustCompile(`(?s)subnet\s+` + subnetPlanTestID +
	`\s+was\s+created\s+and\s+is\s+in\s+state,\s+but\s+reading\s+it\s+back\s+failed.*` +
	`Terraform\s+has\s+marked\s+the\s+subnet\s+tainted,\s+so\s+the\s+next\s+apply\s+deletes\s+it\s+and\s+creates\s+it\s+again\.`)

// regexpSubnetCreatedButJobFailed pins the R145 sentence for a carve the
// platform gave up on after it inserted the row.
var regexpSubnetCreatedButJobFailed = regexp.MustCompile(`(?s)subnet\s+` + subnetPlanTestID +
	`\s+was\s+created\s+and\s+is\s+in\s+state,\s+but\s+its\s+creation\s+job\s+failed.*` +
	`Terraform\s+has\s+marked\s+the\s+subnet\s+tainted,\s+so\s+the\s+next\s+apply\s+deletes\s+it\s+and\s+creates\s+it\s+again\.`)

const (
	subnetPlanTestVpcID        = "11111111-1111-4111-8111-111111111111"
	subnetPlanTestID           = "22222222-2222-4222-8222-222222222222"
	subnetPlanTestDefaultNsg   = "33333333-3333-4333-8333-333333333333"
	subnetPlanTestOtherNsg     = "44444444-4444-4444-8444-444444444444"
	subnetPlanTestCIDR         = "10.50.1.0/24"
	subnetPlanTestTimestamp    = "2026-01-02T15:04:05Z"
	subnetPlanTestRFC850       = "Friday, 02-Jan-26 15:04:05 UTC"
	subnetPlanTestFailedReason = "the provider rejected the allocation"
	gpcnVpcSubnetTest          = "gpcn_vpc_subnet.test"
)

// subnetPlanTestServerState is the row the mock keeps between requests. A read
// after an apply must agree with the configuration, or every step plans a diff.
type subnetPlanTestServerState struct {
	mu          sync.Mutex
	name        string
	description string
	nsgID       string
	nsgName     string
	nicCount    int64
	cidr        string
	rowState    string
	// failureReason stands for the sentence GPCN files against a carve it gave
	// up on. An empty value reads back as the API's null.
	failureReason string
	deleted       bool
	refuseDelete  bool
	// nicCountOnRename stands for an interface that attaches between the
	// refresh and the apply. Zero leaves the census alone.
	nicCountOnRename int64
	// missingOnDelete answers the DELETE with a 404. A subnet another operator
	// already removed gives that answer.
	missingOnDelete bool
	// failCreateJob makes the carve job stop badly. The platform inserts the
	// row before it dispatches that job, so the id exists either way.
	failCreateJob bool
	// failNextRead answers the next listing with a 500. The carve still ran,
	// so the row is there and only the read-back fails.
	failNextRead bool
	createBody   map[string]any
	rebindBody   map[string]any
}

func (s *subnetPlanTestServerState) row() map[string]any {
	var failureReason any
	if s.failureReason != "" {
		failureReason = s.failureReason
	}
	var description any
	if s.description != "" {
		description = s.description
	}
	return map[string]any{
		"id":               subnetPlanTestID,
		"name":             s.name,
		"description":      description,
		"cidr":             s.cidr,
		"state":            s.rowState,
		"nsgId":            s.nsgID,
		"nsgName":          s.nsgName,
		"attachedNicCount": s.nicCount,
		"failureReason":    failureReason,
		"activeJobId":      nil,
		"createdAt":        subnetPlanTestTimestamp,
		"updatedAt":        subnetPlanTestTimestamp,
	}
}

func startSubnetPlanMockServer(t *testing.T) (*httptest.Server, *subnetPlanTestServerState) {
	t.Helper()

	state := &subnetPlanTestServerState{nsgID: subnetPlanTestDefaultNsg, nsgName: "default", rowState: "ready", cidr: subnetPlanTestCIDR}

	collection := "/v1/resource/vpcs/" + subnetPlanTestVpcID + "/subnets/"
	subnetPath := collection + subnetPlanTestID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check":
			testutil.HandleAuthCheck(w)
		case r.Method == http.MethodPost && r.URL.Path == collection:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.createBody = body
			state.name, _ = body["name"].(string)
			state.description, _ = body["description"].(string)
			if requested, ok := body["nsgId"].(string); ok {
				state.nsgID = requested
				state.nsgName = "chosen"
			}
			state.deleted = false
			row := state.row()
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "Operation initiated successfully",
				"data":    map[string]any{"jobId": "job-create", "subnet": row},
			})
		case r.Method == http.MethodGet && r.URL.Path == collection:
			state.mu.Lock()
			failRead := state.failNextRead
			state.failNextRead = false
			rows := []map[string]any{}
			if !state.deleted {
				rows = append(rows, state.row())
			}
			state.mu.Unlock()
			if failRead {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"success":false,"message":"Internal server error","error":{"code":"Internal Error","statusCode":500,"details":null}}`)
				return
			}
			testutil.WriteJSONResponse(w, map[string]any{
				"success": true,
				"message": "",
				"data":    rows,
				"meta":    map[string]any{"total": len(rows), "page": 1, "pageSize": 100, "totalPages": 1},
			})
		case r.Method == http.MethodPut && r.URL.Path == subnetPath:
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.name, _ = body["name"].(string)
			state.description, _ = body["description"].(string)
			if state.nicCountOnRename > 0 {
				state.nicCount = state.nicCountOnRename
			}
			row := state.row()
			state.mu.Unlock()
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "data": row})
		case r.Method == http.MethodPut && r.URL.Path == subnetPath+"/nsg":
			body := testutil.ReadRequestBody(r)
			state.mu.Lock()
			state.rebindBody = body
			state.nsgID, _ = body["nsgId"].(string)
			state.nsgName = "chosen"
			state.mu.Unlock()
			testutil.HandleCreateJobResponse(w, "job-rebind", "Operation initiated successfully")
		case r.Method == http.MethodDelete && r.URL.Path == subnetPath:
			state.mu.Lock()
			missing := state.missingOnDelete
			refuse := state.refuseDelete
			count := state.nicCount
			if missing {
				state.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"Subnet not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
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
				fmt.Fprintf(w, `{"success":false,"message":"Cannot delete a subnet with %d attached network interface(s). Detach or delete the VMs first.","error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`, count)
				return
			}
			testutil.HandleCreateJobResponse(w, "job-delete", "Operation initiated successfully")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			subnetPlanTestHandleJob(w, r, state)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
}

// failTheCreateJob arms the carve failure under the lock. The mock server
// reads the flag from another goroutine.
func (s *subnetPlanTestServerState) failTheCreateJob() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCreateJob = true
}

// failTheNextRead arms the listing failure under the lock. The mock server
// reads the flag from another goroutine.
func (s *subnetPlanTestServerState) failTheNextRead() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNextRead = true
}

// subnetPlanTestHandleJob answers the poll for the job the request names. Only
// the carve job can stop badly, so every other job completes.
func subnetPlanTestHandleJob(w http.ResponseWriter, r *http.Request, state *subnetPlanTestServerState) {
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
				"errorMessage": subnetPlanTestFailedReason,
			}}},
		})
		return
	}

	testutil.HandleJobResponse(w, jobID, subnetPlanTestID, true)
}

// whitespaceRefusal builds the pattern for a whitespace refusal. Terraform
// prints the summary and the detail with the offending line between them.
func whitespaceRefusal(summary, attribute string) *regexp.Regexp {
	loosen := func(text string) string {
		return strings.ReplaceAll(regexp.QuoteMeta(text), " ", `\s+`)
	}
	detail := attribute + " must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"
	return regexp.MustCompile(`(?s)` + loosen(summary) + `.*` + loosen(detail))
}

func subnetPlanTestConfig(host, name, extra string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_subnet" "test" {
  vpc_id = %q
  name   = %q
  cidr   = %q
  %s
}
`, host, subnetPlanTestVpcID, name, subnetPlanTestCIDR, extra)
}

// subnetPlanTestConfigWithoutRetries drops the retry budget. The client retries
// a 500 answer, which would hide a read-back that fails once.
func subnetPlanTestConfigWithoutRetries(host, name string) string {
	config := subnetPlanTestConfig(host, name, "")
	return strings.Replace(config, "  api_key = \"test-key\"\n", "  api_key = \"test-key\"\n  max_retries = 0\n", 1)
}

// subnetPlanTestConfigWithoutCidr asks the allocator for a block rather than
// naming one. That is the only way the prefix reaches the create body.
func subnetPlanTestConfigWithoutCidr(host, name, extra string) string {
	config := subnetPlanTestConfig(host, name, extra)
	return strings.Replace(config, fmt.Sprintf("  cidr   = %q\n", subnetPlanTestCIDR), "", 1)
}

// The subnet is created with its CIDR and read back through the parent
// listing. It is then renamed in place and rebound to another security group.
// The import uses the composite ID the listing read needs.
func TestVpcSubnetResourcePlanCreateReadRenameRebind(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	checkCreateBody := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.createBody == nil {
			return fmt.Errorf("expected a create POST, got none")
		}
		if got, _ := state.createBody["cidr"].(string); got != subnetPlanTestCIDR {
			return fmt.Errorf("expected the create body cidr %q, got %q", subnetPlanTestCIDR, got)
		}
		if _, present := state.createBody["prefix"]; present {
			return fmt.Errorf("expected no prefix key in the create body, got %v", state.createBody["prefix"])
		}
		if _, present := state.createBody["nsgId"]; present {
			return fmt.Errorf("expected no nsgId key in the create body, got %v", state.createBody["nsgId"])
		}
		return nil
	}

	checkRebindBody := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.rebindBody == nil {
			return fmt.Errorf("expected a rebind PUT, got none")
		}
		if got, _ := state.rebindBody["nsgId"].(string); got != subnetPlanTestOtherNsg {
			return fmt.Errorf("expected the rebind body nsgId %q, got %q", subnetPlanTestOtherNsg, got)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-a", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "id", subnetPlanTestID),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", "subnet-plan-a"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", subnetPlanTestCIDR),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "nsg_id", subnetPlanTestDefaultNsg),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "nsg_name", "default"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "state", "ready"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "description", ""),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "attached_nic_count", "0"),
					// The API sends a null failure reason for a subnet that
					// never failed, and the state must keep it null.
					resource.TestCheckNoResourceAttr(gpcnVpcSubnetTest, "failure_reason"),
					// The API never reports the prefix, so it comes from the
					// carved block's mask length.
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "24"),
					checkCreateBody,
				),
			},
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-b", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", "subnet-plan-b"),
			},
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-b", fmt.Sprintf("nsg_id = %q", subnetPlanTestOtherNsg)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "nsg_id", subnetPlanTestOtherNsg),
					checkRebindBody,
				),
			},
			{
				ResourceName:      gpcnVpcSubnetTest,
				ImportState:       true,
				ImportStateId:     subnetPlanTestVpcID + "/" + subnetPlanTestID,
				ImportStateVerify: true,
			},
			{
				Config:             subnetPlanTestConfig(server.URL, "subnet-plan-b", fmt.Sprintf("nsg_id = %q", subnetPlanTestOtherNsg)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// A CIDR and a prefix are the two ways to ask for a block, and the API refuses
// both together. The plan must fail before the request is made.
func TestVpcSubnetResourcePlanRefusesCidrAndPrefix(t *testing.T) {
	t.Parallel()
	server, _ := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      subnetPlanTestConfig(server.URL, "subnet-plan-a", "prefix = 24"),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
		},
	})
}

// A subnet that still holds interfaces cannot be deleted. The operator needs
// the API's own sentence. It names how many interfaces hold the subnet, and
// what to do first.
func TestVpcSubnetResourcePlanSurfacesDeleteRefusal(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-a", ""),
				Check: func(*terraform.State) error {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.nicCount = 2
					state.refuseDelete = true
					return nil
				},
			},
			{
				Config:      subnetPlanTestConfig(server.URL, "subnet-plan-a", ""),
				Destroy:     true,
				ExpectError: regexp.MustCompile(strings.ReplaceAll(`HTTP 409 \(Duplicate Resource\): Cannot delete a subnet with 2 attached network interface\(s\). Detach or delete`, " ", `\s+`)),
			},
		},
	})
}

// A subnet missing from its VPC's listing was deleted outside Terraform. Read
// drops it from state. The next plan then proposes a create rather than an
// update against a row that is gone.
func TestVpcSubnetResourcePlanRecreatesWhenAbsentFromListing(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	config := subnetPlanTestConfig(server.URL, "subnet-plan-a", "")

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
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A subnet that asks for a size rather than a block gets its CIDR from the
// allocator. The carved CIDR lands in state, and the plan that follows is
// empty.
func TestVpcSubnetResourcePlanCarvesFromPrefix(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	config := subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-a", "prefix = 26")

	checkCreateBody := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if got, _ := state.createBody["prefix"].(float64); got != 26 {
			return fmt.Errorf("expected the create body prefix 26, got %v", state.createBody["prefix"])
		}
		if _, present := state.createBody["cidr"]; present {
			return fmt.Errorf("expected no cidr key in the create body, got %v", state.createBody["cidr"])
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", subnetPlanTestCIDR),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "26"),
					checkCreateBody,
				),
			},
			{
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// A NIC can attach between the refresh and the apply. The census is a live
// counter, so the apply writes the fresher number. A plan that pinned the old
// one would end the apply with a result the plan does not allow.
func TestVpcSubnetResourcePlanAcceptsMovedNicCount(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-a", ""),
				Check:  resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "attached_nic_count", "0"),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.nicCountOnRename = 7
				},
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-b", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", "subnet-plan-b"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "attached_nic_count", "7"),
				),
			},
			{
				Config:             subnetPlanTestConfig(server.URL, "subnet-plan-b", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// A subnet another operator already removed answers the DELETE with a 404. The
// destroy must finish, because the row is gone either way.
func TestVpcSubnetResourcePlanTreatsMissingSubnetAsDeleted(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	config := subnetPlanTestConfig(server.URL, "subnet-plan-a", "")

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

// A chosen security group must reach the API on the create. The mapper keeps
// the planned value. A dropped key then writes state that names the wrong
// group.
func TestVpcSubnetResourcePlanSendsChosenNsgOnCreate(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	checkCreateBody := func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.createBody == nil {
			return fmt.Errorf("expected a create POST, got none")
		}
		if got, _ := state.createBody["nsgId"].(string); got != subnetPlanTestOtherNsg {
			return fmt.Errorf("expected the create body nsgId %q, got %q", subnetPlanTestOtherNsg, got)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfig(server.URL, "subnet-plan-a", fmt.Sprintf("nsg_id = %q", subnetPlanTestOtherNsg)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "nsg_id", subnetPlanTestOtherNsg),
					checkCreateBody,
				),
			},
		},
	})
}

// GPCN trims a name, so a configured value with outer whitespace comes back
// different and the plan never settles. The refusal arrives before the request.
// The shared validator changes nothing until the schema carries it.
func TestVpcSubnetResourceSchemaAttachesWhitespaceValidators(t *testing.T) {
	t.Parallel()

	var schemaResponse fwresource.SchemaResponse
	(&vpcSubnetResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, &schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", schemaResponse.Diagnostics)
	}

	assertWhitespaceValidator(t, schemaResponse.Schema.Attributes, "name", "Invalid VPC subnet %s")
	assertWhitespaceValidator(t, schemaResponse.Schema.Attributes, "description", "Invalid VPC subnet %s")
}

func TestVpcSubnetResourcePlanRefusesOuterWhitespace(t *testing.T) {
	t.Parallel()
	server, _ := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      subnetPlanTestConfig(server.URL, " subnet-plan-a", ""),
				ExpectError: whitespaceRefusal("Invalid VPC subnet name", "name"),
			},
			{
				Config:      subnetPlanTestConfig(server.URL, "subnet-plan-a", `description = " web tier"`),
				ExpectError: whitespaceRefusal("Invalid VPC subnet description", "description"),
			},
		},
	})
}

// A carve GPCN gave up on still reads back cleanly. The operator learns of it
// through the state and the reason the API files against the row.
func TestVpcSubnetResourcePlanReadsFailedSubnet(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	config := subnetPlanTestConfig(server.URL, "subnet-plan-a", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "state", "ready"),
					resource.TestCheckNoResourceAttr(gpcnVpcSubnetTest, "failure_reason"),
				),
			},
			{
				PreConfig: func() {
					state.mu.Lock()
					defer state.mu.Unlock()
					state.rowState = "failed"
					state.failureReason = "the provider rejected the allocation"
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "state", "failed"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "failure_reason", "the provider rejected the allocation"),
				),
			},
		},
	})
}

// Terraform reconciles a description in place, so Read must show the one GPCN
// holds. A refresh that kept the stale value would plan nothing and leave the
// drift in place.
func TestVpcSubnetResourcePlanRefreshesDescription(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	config := subnetPlanTestConfig(server.URL, "subnet-plan-a", `description = "web tier"`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "description", "web tier"),
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
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "description", "web tier"),
			},
		},
	})
}

// The API never reports the prefix. An import leaves it null, and a
// configuration that names one plans a replacement. The carved CIDR's mask
// length is the same number, so the import reads the prefix from it.
func TestVpcSubnetResourcePlanImportFillsPrefixFromCidr(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	state.mu.Lock()
	state.cidr = "10.50.1.0/26"
	state.mu.Unlock()

	config := subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-a", "prefix = 26")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", "10.50.1.0/26"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "26"),
				),
			},
			{
				ResourceName:      gpcnVpcSubnetTest,
				ImportState:       true,
				ImportStateId:     subnetPlanTestVpcID + "/" + subnetPlanTestID,
				ImportStateVerify: true,
			},
			{
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// A subnet that names neither a CIDR nor a prefix takes the allocator's
// default. The mask length of the carved block is the prefix. An import lands
// on the same number, so the plan that follows it is empty.
func TestVpcSubnetResourcePlanFillsPrefixWithoutEitherKey(t *testing.T) {
	t.Parallel()
	server, _ := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-a", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", subnetPlanTestCIDR),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "24"),
				),
			},
			{
				Config: subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-b", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "24"),
			},
			{
				ResourceName:      gpcnVpcSubnetTest,
				ImportState:       true,
				ImportStateId:     subnetPlanTestVpcID + "/" + subnetPlanTestID,
				ImportStateVerify: true,
			},
			{
				Config:             subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-b", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// An import block plans against the imported state, which the import command
// never does. Only that arm shows a configuration shape whose prefix the
// mapper left null. The kind excludes ImportStateVerify, so it needs a test of
// its own beside the two import-command cases.
func TestVpcSubnetResourcePlanImportBlockIsANoOpForEveryShape(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name       string
		carvedCIDR string
		config     func(host string) string
	}{
		{
			name:       "cidr only",
			carvedCIDR: subnetPlanTestCIDR,
			config:     func(host string) string { return subnetPlanTestConfig(host, "subnet-plan-a", "") },
		},
		{
			name:       "prefix only",
			carvedCIDR: "10.50.1.0/26",
			config: func(host string) string {
				return subnetPlanTestConfigWithoutCidr(host, "subnet-plan-a", "prefix = 26")
			},
		},
		{
			name:       "neither",
			carvedCIDR: subnetPlanTestCIDR,
			config:     func(host string) string { return subnetPlanTestConfigWithoutCidr(host, "subnet-plan-a", "") },
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			server, state := startSubnetPlanMockServer(t)

			state.mu.Lock()
			state.cidr = shape.carvedCIDR
			state.mu.Unlock()

			config := shape.config(server.URL)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						Config:          config,
						ResourceName:    gpcnVpcSubnetTest,
						ImportState:     true,
						ImportStateKind: resource.ImportBlockWithID,
						ImportStateId:   subnetPlanTestVpcID + "/" + subnetPlanTestID,
					},
				},
			})
		})
	}
}

// A prefix that differs from the mask of the block GPCN carved asks for
// another block. GPCN cannot re-carve one in place. The plan therefore
// replaces the subnet rather than proposing an update the API refuses.
func TestVpcSubnetResourcePlanReplacesOnChangedPrefix(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)

	state.mu.Lock()
	state.cidr = "10.50.1.0/26"
	state.mu.Unlock()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-a", "prefix = 26"),
				Check:  resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "prefix", "26"),
			},
			{
				ResourceName:      gpcnVpcSubnetTest,
				ImportState:       true,
				ImportStateId:     subnetPlanTestVpcID + "/" + subnetPlanTestID,
				ImportStateVerify: true,
			},
			{
				Config: subnetPlanTestConfigWithoutCidr(server.URL, "subnet-plan-a", "prefix = 27"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// The plan harness carries no warning assertion, so Read is driven directly. A
// test on the constructor alone leaves the call site unguarded.
func TestVpcSubnetReadWarnsOnFailedSubnetUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	readInState := func(rowState, failureReason string) fwresource.ReadResponse {
		t.Helper()

		row := &subnetPlanTestServerState{
			name:          "subnet-plan-a",
			nsgID:         subnetPlanTestDefaultNsg,
			nsgName:       "default",
			cidr:          subnetPlanTestCIDR,
			rowState:      rowState,
			failureReason: failureReason,
		}

		_, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
			T: t,
			Handler: func(w http.ResponseWriter, _ *http.Request) {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "",
					"data":    []map[string]any{row.row()},
					"meta":    map[string]any{"total": 1, "page": 1, "pageSize": 100, "totalPages": 1},
				})
			},
		})

		subnetResource := &vpcSubnetResource{client: gpcnClient}

		var schemaResponse fwresource.SchemaResponse
		subnetResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

		priorState := tfsdk.State{Schema: schemaResponse.Schema}
		diags := priorState.Set(ctx, vpcsubnets.ResourceModel{
			ID:               types.StringValue(subnetPlanTestID),
			VpcID:            types.StringValue(subnetPlanTestVpcID),
			Name:             types.StringValue("subnet-plan-a"),
			Description:      types.StringValue(""),
			CIDR:             types.StringValue(subnetPlanTestCIDR),
			Prefix:           types.Int64Value(24),
			NsgID:            types.StringValue(subnetPlanTestDefaultNsg),
			NsgName:          types.StringValue("default"),
			State:            types.StringValue("ready"),
			AttachedNicCount: types.Int64Value(0),
			FailureReason:    types.StringNull(),
			CreatedTime:      types.StringValue(subnetPlanTestRFC850),
			LastUpdated:      types.StringValue(subnetPlanTestRFC850),
		})
		if diags.HasError() {
			t.Fatalf("failed to build the prior state: %v", diags)
		}

		readResponse := fwresource.ReadResponse{
			State: tfsdk.State{Schema: schemaResponse.Schema, Raw: priorState.Raw},
		}
		subnetResource.Read(ctx, fwresource.ReadRequest{State: priorState}, &readResponse)

		if readResponse.Diagnostics.HasError() {
			t.Fatalf("Read reported errors: %v", readResponse.Diagnostics.Errors())
		}
		return readResponse
	}

	failed := readInState("failed", subnetPlanTestFailedReason)
	warnings := failed.Diagnostics.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if got := warnings[0].Summary(); got != "GPCN VPC subnet is in the failed state" {
		t.Errorf("summary = %q, want %q", got, "GPCN VPC subnet is in the failed state")
	}
	wantDetail := "Subnet '22222222-2222-4222-8222-222222222222' is in the 'failed' state, so it carries no working network. GPCN gives this reason: the provider rejected the allocation. Delete the subnet and create it again, or contact GPCN support."
	if got := warnings[0].Detail(); got != wantDetail {
		t.Errorf("detail = %q, want %q", got, wantDetail)
	}

	// The platform can park a carve with no reason recorded.
	noReason := readInState("failed", "")
	noReasonWarnings := noReason.Diagnostics.Warnings()
	if len(noReasonWarnings) != 1 {
		t.Fatalf("warnings with no reason = %v, want exactly one", noReasonWarnings)
	}
	wantNoReasonDetail := "Subnet '22222222-2222-4222-8222-222222222222' is in the 'failed' state, so it carries no working network. GPCN recorded no reason. Delete the subnet and create it again, or contact GPCN support."
	if got := noReasonWarnings[0].Detail(); got != wantNoReasonDetail {
		t.Errorf("detail with no reason = %q, want %q", got, wantNoReasonDetail)
	}

	ready := readInState("ready", "")
	if got := ready.Diagnostics.WarningsCount(); got != 0 {
		t.Errorf("warnings for a ready subnet = %d, want 0", got)
	}
}

// The platform inserts the subnet row before it dispatches the carve job. The
// row then holds the name and the CIDR until someone deletes it. A failed job
// must therefore leave the id in state. The destroy then deletes the row
// instead of leaving the next apply to collide with it.
func TestVpcSubnetResourcePlanKeepsTheIdWhenTheCreateJobFails(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)
	state.failTheCreateJob()

	config := subnetPlanTestConfig(server.URL, "carve-failed", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpSubnetCreatedButJobFailed,
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkSubnetDestroyDeletedTheRow(state),
			},
		},
	})
}

// checkSubnetDestroyDeletedTheRow proves the failed create wrote the id to
// state. The mock routes the delete by that id. A create that keeps the id to
// itself leaves the destroy nothing to delete.
func checkSubnetDestroyDeletedTheRow(state *subnetPlanTestServerState) func(*terraform.State) error {
	return func(*terraform.State) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if !state.deleted {
			return fmt.Errorf("expected the destroy to delete the subnet row, but the mock still lists it")
		}
		return nil
	}
}

// The platform answers the read-back after a carve that worked. A failure
// there leaves the row and the id in state. The error therefore carries the
// tainted remedy, not a bare HTTP failure.
func TestVpcSubnetResourcePlanKeepsTheIdWhenTheReadBackFails(t *testing.T) {
	t.Parallel()
	server, state := startSubnetPlanMockServer(t)
	state.failTheNextRead()

	config := subnetPlanTestConfigWithoutRetries(server.URL, "read-back-failed")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpSubnetCreatedButReadBackFailed,
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkSubnetDestroyDeletedTheRow(state),
			},
		},
	})
}

// The pre-poll write nulls the Computed attributes the carve has not filled
// yet. A prefix the configuration names is already known, so nulling it would
// plan a replacement on the next apply.
func TestVpcSubnetCreateKeepsTheConfiguredPrefixWhenTheCarveFailsUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	row := &subnetPlanTestServerState{
		name:     "prefix-carve",
		nsgID:    subnetPlanTestDefaultNsg,
		nsgName:  "default",
		cidr:     "10.50.1.0/26",
		rowState: "creating",
	}

	_, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Job status retrieved",
					"data": map[string]any{"jobs": []map[string]any{{
						"jobId":        "job-create",
						"isCompleted":  false,
						"isTerminal":   true,
						"hasFailed":    true,
						"errorMessage": subnetPlanTestFailedReason,
					}}},
				})
			case r.Method == http.MethodPost:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": "job-create", "subnet": row.row()},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	subnetResource := &vpcSubnetResource{client: gpcnClient}

	var schemaResponse fwresource.SchemaResponse
	subnetResource.Schema(ctx, fwresource.SchemaRequest{}, &schemaResponse)

	plan := tfsdk.Plan{Schema: schemaResponse.Schema}
	diags := plan.Set(ctx, vpcsubnets.ResourceModel{
		ID:               types.StringUnknown(),
		VpcID:            types.StringValue(subnetPlanTestVpcID),
		Name:             types.StringValue("prefix-carve"),
		Description:      types.StringValue(""),
		CIDR:             types.StringUnknown(),
		Prefix:           types.Int64Value(26),
		NsgID:            types.StringUnknown(),
		NsgName:          types.StringUnknown(),
		State:            types.StringUnknown(),
		AttachedNicCount: types.Int64Unknown(),
		FailureReason:    types.StringUnknown(),
		CreatedTime:      types.StringUnknown(),
		LastUpdated:      types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatalf("failed to build the plan: %v", diags)
	}

	createResponse := fwresource.CreateResponse{
		State: tfsdk.State{Schema: schemaResponse.Schema, Raw: plan.Raw},
	}
	subnetResource.Create(ctx, fwresource.CreateRequest{Plan: plan}, &createResponse)

	if !createResponse.Diagnostics.HasError() {
		t.Fatalf("expected the failed carve to report an error, got none")
	}

	var stored vpcsubnets.ResourceModel
	if diags := createResponse.State.Get(ctx, &stored); diags.HasError() {
		t.Fatalf("failed to read the written state: %v", diags)
	}

	if got := stored.Prefix; !got.Equal(types.Int64Value(26)) {
		t.Errorf("prefix = %v, want 26", got)
	}
	// The carve has not run, so the attributes only it fills stay null.
	if !stored.State.IsNull() {
		t.Errorf("state = %v, want null", stored.State)
	}
	if !stored.AttachedNicCount.IsNull() {
		t.Errorf("attached_nic_count = %v, want null", stored.AttachedNicCount)
	}
}
