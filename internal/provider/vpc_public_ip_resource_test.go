package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var gpcnVPCPublicIpTest = "gpcn_vpc_public_ip.test"

// vpcTestIDEnvVar names an existing GPCN VPC. An elastic address is always a
// child of a VPC, and this suite does not create one.
const vpcTestIDEnvVar = "GPCN_TEST_VPC_ID"

// TestVPCPublicIpResource acquires an address in an existing VPC and releases
// it again. The address is a holding for the whole case, so no machine loses
// connectivity when the run ends.
func TestVPCPublicIpResource(t *testing.T) {
	t.Parallel()
	vpcID := os.Getenv(vpcTestIDEnvVar)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if vpcID == "" {
				t.Skipf("%s is not set: name an existing GPCN VPC to acquire an address in", vpcTestIDEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "gpcn_vpc_public_ip" "test" {
  vpc_id = "` + vpcID + `"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "vpc_id", vpcID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpTest, "held", "true"),
					resource.TestCheckResourceAttrSet(gpcnVPCPublicIpTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVPCPublicIpTest, "state"),
					resource.TestCheckResourceAttrSet(gpcnVPCPublicIpTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVPCPublicIpTest, "last_updated"),
				),
			},
		},
	})
}
