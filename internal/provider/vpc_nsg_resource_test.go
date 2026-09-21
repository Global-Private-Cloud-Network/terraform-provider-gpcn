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
func vpcNsgAccTestConfig(vpcName, nsgName, rules string) string {
	return providerConfig + fmt.Sprintf(`
data "gpcn_datacenters" "test" {
  name = "Chicago"
}

resource "gpcn_vpc" "test" {
  name          = %q
  datacenter_id = data.gpcn_datacenters.test.datacenters[0].id
  cidr          = "10.61.0.0/16"
}

resource "gpcn_vpc_nsg" "test" {
  vpc_id      = gpcn_vpc.test.id
  name        = %q
  description = "Created by acceptance tests"
%s
}
`, vpcName, nsgName, rules)
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
	vpcNsgAccTestRulePing = `
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = "10.61.0.0/16"
  }`
)

func TestVpcNsgResource(t *testing.T) {
	t.Parallel()
	suffix := acctest.RandString(8)
	vpcName := fmt.Sprintf("tf-vpc-%s", suffix)
	nsgName := fmt.Sprintf("tf-nsg-%s", suffix)
	nsgNameUpdated := fmt.Sprintf("tf-nsg-updated-%s", suffix)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcNsgAccTestConfig(vpcName, nsgName, vpcNsgAccTestRuleHTTPS),
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
				// A rename and a rule addition in one apply: the rename is
				// synchronous and the rules replace runs as a job.
				Config: vpcNsgAccTestConfig(vpcName, nsgNameUpdated, vpcNsgAccTestRuleHTTPS+vpcNsgAccTestRulePing),
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
				Config: vpcNsgAccTestConfig(vpcName, nsgNameUpdated, vpcNsgAccTestRulePing),
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
