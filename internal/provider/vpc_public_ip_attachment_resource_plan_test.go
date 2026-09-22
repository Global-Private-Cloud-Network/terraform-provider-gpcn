package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The 422 is the platform's mirror wall between the VPC verbs and the legacy
// per-interface address. The provider forwards its sentence unchanged.
var regexpPublicIpsAttachToVPCInterfacesOnly = regexp.MustCompile(`Public\s+IPs\s+attach\s+to\s+VPC\s+interfaces\s+only`)

// A read-back that fails names both the address and the interface it bound.
// The operator needs both to undo the binding by hand.
var regexpPublicIpAttachedButReadBackFailed = regexp.MustCompile(`(?s)public\s+IP\s+` + vpcPublicIpPlanTestID +
	`\s+was\s+attached\s+to\s+network\s+interface\s+` + vpcPublicIpPlanTestNicID +
	`\s+and\s+is\s+in\s+state,\s+but\s+reading\s+it\s+back\s+failed:.*` +
	`Terraform\s+has\s+marked\s+the\s+attachment\s+tainted,\s+so\s+the\s+next\s+apply\s+detaches\s+it\s+and\s+attaches\s+again\.`)

func vpcPublicIpAttachmentPlanTestConfig(host string) string {
	return vpcPublicIpAttachmentPlanTestConfigForNic(host, vpcPublicIpPlanTestNicID)
}

func vpcPublicIpAttachmentPlanTestConfigForNic(host, nicID string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_vpc_public_ip_attachment" "test" {
  vpc_id       = %q
  public_ip_id = %q
  nic_id       = %q
}
`, host, vpcPublicIpPlanTestVpcID, vpcPublicIpPlanTestID, nicID)
}

// vpcPublicIpAttachmentPlanTestConfigWithoutRetries turns the client retries
// off, so one refused listing is one failed read rather than four.
func vpcPublicIpAttachmentPlanTestConfigWithoutRetries(host string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host        = %q
  api_key     = "test-key"
  max_retries = 0
}

resource "gpcn_vpc_public_ip_attachment" "test" {
  vpc_id       = %q
  public_ip_id = %q
  nic_id       = %q
}
`, host, vpcPublicIpPlanTestVpcID, vpcPublicIpPlanTestID, vpcPublicIpPlanTestNicID)
}

// The attach names the interface, and the state records the machine the
// platform bound the address to.
func TestVPCPublicIpAttachmentResourcePlanAttachReadAndDetach(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.becomeReady()
	config := vpcPublicIpAttachmentPlanTestConfig(server.URL)

	checkAttachBody := func(*terraform.State) error {
		row.mu.Lock()
		defer row.mu.Unlock()
		if row.lastAttachBody == nil {
			return fmt.Errorf("expected an attach POST, got none")
		}
		if got, _ := row.lastAttachBody["nicId"].(string); got != vpcPublicIpPlanTestNicID {
			return fmt.Errorf("expected attach body nicId %q, got %q", vpcPublicIpPlanTestNicID, got)
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy:             checkAttachmentDestroyDetachedOnly(row),
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "id", vpcPublicIpPlanTestID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "public_ip_id", vpcPublicIpPlanTestID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "nic_id", vpcPublicIpPlanTestNicID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "virtual_machine_id", vpcPublicIpPlanTestVmID),
					checkAttachBody,
				),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionNoop),
					},
				},
			},
		},
	})
}

// checkAttachmentDestroyDetachedOnly pins the destroy verb. A release would
// give the billable address back to the platform. It would also orphan the
// gpcn_vpc_public_ip that still owns the address.
func checkAttachmentDestroyDetachedOnly(row *publicIpPlanTestRow) func(*terraform.State) error {
	return func(*terraform.State) error {
		if row.detachCallCount() == 0 {
			return fmt.Errorf("expected the destroy to detach the address, got no detach request")
		}
		if got := row.releaseCallCount(); got != 0 {
			return fmt.Errorf("expected the destroy to send no release, got %d", got)
		}
		if row.isReleased() {
			return fmt.Errorf("expected the address to survive the destroy, but the mock reports it released")
		}
		return nil
	}
}

// GPCN has no verb that re-points a live address. A new nic_id must therefore
// plan a replacement, not an in-place update.
func TestVPCPublicIpAttachmentResourcePlanReplacesOnNicIdChange(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.becomeReady()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcPublicIpAttachmentPlanTestConfig(server.URL),
			},
			{
				Config:             vpcPublicIpAttachmentPlanTestConfigForNic(server.URL, vpcPublicIpPlanTestOtherNic),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

// An address detached outside Terraform holds no machine. The attachment is
// gone, and the next plan attaches it again.
func TestVPCPublicIpAttachmentResourcePlanRemovesDetachedAttachmentFromState(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.becomeReady()
	config := vpcPublicIpAttachmentPlanTestConfig(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: row.detach,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// An address released outside Terraform leaves the listing. The
			// read must drop the attachment as well.
			{
				PreConfig:          row.release,
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// A legacy interface is not a VPC interface, and the platform refuses the
// attach with its own sentence.
func TestVPCPublicIpAttachmentResourcePlanSurfacesAttachRefusal(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.becomeReady()
	row.refuseAttach(http.StatusUnprocessableEntity, "Public IPs attach to VPC interfaces only")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      vpcPublicIpAttachmentPlanTestConfig(server.URL),
				ExpectError: regexpPublicIpsAttachToVPCInterfacesOnly,
			},
		},
	})
}

// GPCN refuses a second attach until the address detaches. A binding the
// read-back could not confirm must still reach state. The destroy then
// detaches it instead of wedging every later apply.
func TestVPCPublicIpAttachmentResourcePlanKeepsBindingWhenReadBackFails(t *testing.T) {
	t.Parallel()
	server, row := startVPCPublicIpPlanMockServer(t)
	row.becomeReady()
	row.failListingGets()

	config := vpcPublicIpAttachmentPlanTestConfigWithoutRetries(server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexpPublicIpAttachedButReadBackFailed,
			},
			{
				PreConfig: row.healListingGets,
				Config:    config,
				Destroy:   true,
				Check:     checkAttachmentDestroySentOneDetach(row),
			},
		},
	})
}

// checkAttachmentDestroySentOneDetach proves the failed read-back wrote the
// binding to state. A create that keeps it to itself leaves the destroy
// nothing to detach. The address then stays bound outside Terraform.
func checkAttachmentDestroySentOneDetach(row *publicIpPlanTestRow) func(*terraform.State) error {
	return func(*terraform.State) error {
		if got := row.detachCallCount(); got != 1 {
			return fmt.Errorf("expected the destroy to send exactly one detach, got %d", got)
		}
		if got := row.releaseCallCount(); got != 0 {
			return fmt.Errorf("expected the destroy to send no release, got %d", got)
		}
		return nil
	}
}
