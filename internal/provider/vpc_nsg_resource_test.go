package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// vpcNsgAccTestConfig builds a group inside a VPC the same configuration
// creates. A group has no datacenter of its own, so the VPC is what places it.
// The caller draws the range, because the overlap check covers the whole
// entity.
func vpcNsgAccTestConfig(vpcName, vpcCidr, nsgName, rules string) string {
	return providerConfig + fmt.Sprintf(`
data "gpcn_datacenters" "test" {
  name = "Chicago"
}

resource "gpcn_vpc" "test" {
  name                = %q
  datacenter_id       = data.gpcn_datacenters.test.datacenters[0].id
  cidr                = %q
  acknowledge_overlap = true
}

resource "gpcn_vpc_nsg" "test" {
  vpc_id      = gpcn_vpc.test.id
  name        = %q
  description = "Created by acceptance tests"
%s
}
`, vpcName, vpcCidr, nsgName, rules)
}

const (
	vpcNsgAccTestRuleHTTPS = `
  rule {
    direction      = "ingress"
    protocol       = "tcp"
    port_range_min = 443
    port_range_max = 443
    remote_cidr    = "0.0.0.0/0"
    description    = "https"
  }`
)

// vpcNsgAccTestRulePing admits ping from inside the VPC, so it needs the range
// the case drew.
func vpcNsgAccTestRulePing(vpcCidr string) string {
	return fmt.Sprintf(`
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = %q
  }`, vpcCidr)
}

// vpcNsgAccOctetSlot names the block each acceptance case in this file owns. A
// case that draws its block at random meets an earlier run about half the
// time. The repository has no sweepers.
var vpcNsgAccOctetSlot = map[string]int{
	"TestVpcNsgResource": 0,
}

// This file owns the window at base 96. The VPC, subnet and machine files own
// 64, 80 and 112.
const (
	vpcNsgAccOctetBase  = 96
	vpcNsgAccOctetSlots = 16
)

// vpcNsgAccOctetFor returns the octet of the named case. It fails a case
// the table does not name.
func vpcNsgAccOctetFor(t testing.TB, name string) int {
	t.Helper()
	slot, named := vpcNsgAccOctetSlot[name]
	if !named {
		t.Fatalf("Expected %s to own a slot in vpcNsgAccOctetSlot", name)
	}
	return vpcNsgAccOctetBase + slot
}

// The guard reads the table alone, not the call sites. It refuses a slot
// outside this file's window, because that slot takes a block another file
// owns. It refuses two entries that take one octet. The compiler accepts
// both.
func TestVpcNsgAcceptanceOctetsAreUnique(t *testing.T) {
	owner := map[int]string{}
	for name, slot := range vpcNsgAccOctetSlot {
		if slot < 0 || slot >= vpcNsgAccOctetSlots {
			t.Errorf("Expected %s to take a slot below %d, got %d", name, vpcNsgAccOctetSlots, slot)
		}
		octet := vpcNsgAccOctetFor(t, name)
		if other, taken := owner[octet]; taken {
			t.Errorf("Expected %s and %s to take different octets, both take %d", name, other, octet)
		}
		owner[octet] = name
	}
}

func TestVpcNsgResource(t *testing.T) {
	t.Parallel()
	suffix := acctest.RandString(8)
	vpcName := fmt.Sprintf("tf-vpc-%s", suffix)
	nsgName := fmt.Sprintf("tf-nsg-%s", suffix)
	nsgNameUpdated := fmt.Sprintf("tf-nsg-updated-%s", suffix)
	// A fixed block meets only what an earlier run of this case left behind,
	// which the configuration acknowledges.
	vpcCidr := fmt.Sprintf("10.%d.0.0/16", vpcNsgAccOctetFor(t, t.Name()))
	pingRule := vpcNsgAccTestRulePing(vpcCidr)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcNsgAccTestConfig(vpcName, vpcCidr, nsgName, vpcNsgAccTestRuleHTTPS),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "name", nsgName),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "is_default", "false"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule_count", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "subnet_count", "0"),
					resource.TestCheckResourceAttrSet(gpcnVpcNsgTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVpcNsgTest, "created_time"),
				),
			},
			{
				// One apply carries a rename and a rule addition. The rename is
				// synchronous, and the rules replace runs as a job.
				Config: vpcNsgAccTestConfig(vpcName, vpcCidr, nsgNameUpdated, vpcNsgAccTestRuleHTTPS+pingRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcNsgTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "name", nsgNameUpdated),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "2"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule_count", "2"),
				),
			},
			{
				// Dropping a rule proves the replace carries the whole set.
				Config: vpcNsgAccTestConfig(vpcName, vpcCidr, nsgNameUpdated, pingRule),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule.#", "1"),
					resource.TestCheckResourceAttr(gpcnVpcNsgTest, "rule_count", "1"),
				),
			},
			{
				ResourceName:            gpcnVpcNsgTest,
				ImportState:             true,
				ImportStateIdFunc:       vpcNsgAccTestImportID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
		},
	})
}

// The import ID is the composite the read needs, because a security group row
// carries no VPC ID.
func vpcNsgAccTestImportID(state *terraform.State) (string, error) {
	nsg, ok := state.RootModule().Resources[gpcnVpcNsgTest]
	if !ok {
		return "", fmt.Errorf("resource %s is not in the state", gpcnVpcNsgTest)
	}
	return nsg.Primary.Attributes["vpc_id"] + "/" + nsg.Primary.ID, nil
}
