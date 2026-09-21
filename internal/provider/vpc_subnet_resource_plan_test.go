package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	subnetPlanTestVpcID      = "11111111-1111-4111-8111-111111111111"
	subnetPlanTestID         = "22222222-2222-4222-8222-222222222222"
	subnetPlanTestDefaultNsg = "33333333-3333-4333-8333-333333333333"
	subnetPlanTestOtherNsg   = "44444444-4444-4444-8444-444444444444"
	subnetPlanTestCIDR       = "10.50.1.0/24"
	subnetPlanTestTimestamp  = "2026-01-02T15:04:05Z"
	gpcnVpcSubnetTest        = "gpcn_vpc_subnet.test"
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
	createBody      map[string]any
	rebindBody      map[string]any
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
		"cidr":             subnetPlanTestCIDR,
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

	state := &subnetPlanTestServerState{nsgID: subnetPlanTestDefaultNsg, nsgName: "default", rowState: "ready"}

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
			rows := []map[string]any{}
			if !state.deleted {
				rows = append(rows, state.row())
			}
			state.mu.Unlock()
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
				fmt.Fprint(w, `{"success":false,"message":"Subnet not found","error":{"code":"RESOURCE_NOT_FOUND","statusCode":404,"details":null}}`)
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
				fmt.Fprintf(w, `{"success":false,"message":"Cannot delete a subnet with %d attached network interface(s). Detach or delete the VMs first.","error":{"code":"DUPLICATE_RESOURCE","statusCode":409,"details":null}}`, count)
				return
			}
			testutil.HandleCreateJobResponse(w, "job-delete", "Operation initiated successfully")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", subnetPlanTestID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, state
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

// The subnet is created with its CIDR, read back through the parent listing,
// renamed in place, rebound to another security group and imported by the
// composite ID its listing read needs.
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
					resource.TestCheckNoResourceAttr(gpcnVpcSubnetTest, "prefix"),
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

// A subnet that still holds interfaces cannot be deleted, and the operator
// needs the API's own sentence: it names how many, and what to do first.
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
				ExpectError: regexp.MustCompile(strings.ReplaceAll(`Cannot delete a subnet with 2 attached network interface\(s\). Detach or delete`, " ", `\s+`)),
			},
		},
	})
}

// A subnet missing from its VPC's listing was deleted outside Terraform. Read
// drops it from state, so the next plan proposes a create rather than an
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

	config := subnetPlanTestConfig(server.URL, "subnet-plan-a", "prefix = 26")
	config = strings.Replace(config, fmt.Sprintf("  cidr   = %q\n", subnetPlanTestCIDR), "", 1)

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
func TestVpcSubnetResourcePlanRefusesOuterWhitespace(t *testing.T) {
	t.Parallel()
	server, _ := startSubnetPlanMockServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      subnetPlanTestConfig(server.URL, " subnet-plan-a", ""),
				ExpectError: regexp.MustCompile(strings.ReplaceAll(regexp.QuoteMeta("name must not start or end with whitespace (GPCN trims it, which would make the stored value differ from the configuration)"), " ", `\s+`)),
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
