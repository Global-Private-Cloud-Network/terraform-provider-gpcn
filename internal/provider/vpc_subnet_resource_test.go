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
// it.
func vpcSubnetAccTestConfig(vpcName, subnetName, description string) string {
	return providerConfig + fmt.Sprintf(`
data "gpcn_datacenters" "test" {
  name = "Chicago"
}

resource "gpcn_vpc" "test" {
  name          = %q
  datacenter_id = data.gpcn_datacenters.test.datacenters[0].id
  cidr          = "10.60.0.0/16"
}

resource "gpcn_vpc_subnet" "test" {
  vpc_id      = gpcn_vpc.test.id
  name        = %q
  description = %q
  cidr        = "10.60.1.0/24"
}
`, vpcName, subnetName, description)
}

func TestVpcSubnetResource(t *testing.T) {
	t.Parallel()
	suffix := acctest.RandString(8)
	vpcName := fmt.Sprintf("tf-vpc-%s", suffix)
	subnetName := fmt.Sprintf("tf-subnet-%s", suffix)
	subnetNameUpdated := fmt.Sprintf("tf-subnet-updated-%s", suffix)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: vpcSubnetAccTestConfig(vpcName, subnetName, "Created by acceptance tests"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcSubnetTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "name", subnetName),
					resource.TestCheckResourceAttr(gpcnVpcSubnetTest, "cidr", "10.60.1.0/24"),
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
				Config: vpcSubnetAccTestConfig(vpcName, subnetNameUpdated, "Updated by acceptance tests"),
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
