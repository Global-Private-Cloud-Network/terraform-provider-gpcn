package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// vpcSubnetAccTestConfig carves one subnet out of a VPC the same configuration
// creates. The subnet has no datacenter of its own, so the VPC is what places
// it. The caller draws both ranges, because the overlap check covers the whole
// entity.
func vpcSubnetAccTestConfig(vpcName, vpcCidr, subnetName, subnetCidr, description string) string {
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

resource "gpcn_vpc_subnet" "test" {
  vpc_id      = gpcn_vpc.test.id
  name        = %q
  description = %q
  cidr        = %q
}
`, vpcName, vpcCidr, subnetName, description, subnetCidr)
}

// vpcSubnetAccOctetSlot names the block each acceptance case in this file
// owns. A case that draws its block at random meets an earlier run about half
// the time. The repository has no sweepers.
var vpcSubnetAccOctetSlot = map[string]int{
	"TestVpcSubnetResource": 0,
}

// This file owns the window at base 80. The VPC, group and machine files own
// 64, 96 and 112.
const vpcSubnetAccOctetBase = 80

// vpcSubnetAccOctetFor returns the octet of the named case. Every call site
// reads this one function, so a case cannot take a block the table never gave
// it.
func vpcSubnetAccOctetFor(t testing.TB, name string) int {
	t.Helper()
	slot, named := vpcSubnetAccOctetSlot[name]
	if !named {
		t.Fatalf("Expected %s to own a slot in vpcSubnetAccOctetSlot", name)
	}
	return vpcSubnetAccOctetBase + slot
}

func TestVpcSubnetResource(t *testing.T) {
	t.Parallel()
	suffix := acctest.RandString(8)
	vpcName := fmt.Sprintf("tf-vpc-%s", suffix)
	subnetName := fmt.Sprintf("tf-subnet-%s", suffix)
	subnetNameUpdated := fmt.Sprintf("tf-subnet-updated-%s", suffix)
	// A fixed block meets only what an earlier run of this case left behind,
	// which the configuration acknowledges.
	octet := vpcSubnetAccOctetFor(t, t.Name())
	vpcCidr := fmt.Sprintf("10.%d.0.0/16", octet)
	subnetCidr := fmt.Sprintf("10.%d.1.0/24", octet)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcSubnetAccTestConfig(vpcName, vpcCidr, subnetName, subnetCidr, "Created by acceptance tests"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", subnetName),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", subnetCidr),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "state", "ready"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "attached_nic_count", "0"),
					resource.TestCheckResourceAttrSet(gpcnVpcSubnetTest, "id"),
					// An omitted nsg_id binds the VPC's own default group.
					resource.TestCheckResourceAttrSet(gpcnVpcSubnetTest, "nsg_id"),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "nsg_name", "default"),
					resource.TestCheckResourceAttrSet(gpcnVpcSubnetTest, "created_time"),
				),
			},
			{
				Config: vpcSubnetAccTestConfig(vpcName, vpcCidr, subnetNameUpdated, subnetCidr, "Updated by acceptance tests"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", subnetNameUpdated),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "description", "Updated by acceptance tests"),
				),
			},
			{
				ResourceName:            gpcnVpcSubnetTest,
				ImportState:             true,
				ImportStateIdFunc:       vpcSubnetAccTestImportID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
		},
	})
}

// The import ID is the composite the listing read needs, because a subnet row
// carries no VPC ID.
func vpcSubnetAccTestImportID(state *terraform.State) (string, error) {
	subnet, ok := state.RootModule().Resources[gpcnVpcSubnetTest]
	if !ok {
		return "", fmt.Errorf("resource %s is not in the state", gpcnVpcSubnetTest)
	}
	return subnet.Primary.Attributes["vpc_id"] + "/" + subnet.Primary.ID, nil
}
