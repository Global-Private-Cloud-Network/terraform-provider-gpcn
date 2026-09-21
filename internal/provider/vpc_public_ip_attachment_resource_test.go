package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var gpcnVPCPublicIpAttachmentTest = "gpcn_vpc_public_ip_attachment.test"

// vpcNicTestIDEnvVar names an existing VPC network interface. The address the
// case acquires is attached to it and detached again.
const vpcNicTestIDEnvVar = "GPCN_TEST_VPC_NIC_ID"

// TestVPCPublicIpAttachmentResource acquires an address, attaches it to a
// running machine's interface and detaches it. The destroy leaves the address
// held, and the gpcn_vpc_public_ip resource then releases it. The case does not
// assert held on the address, because the address resource keeps the state its
// own create wrote until the next refresh.
func TestVPCPublicIpAttachmentResource(t *testing.T) {
	t.Parallel()
	vpcID := os.Getenv(vpcTestIDEnvVar)
	nicID := os.Getenv(vpcNicTestIDEnvVar)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if vpcID == "" || nicID == "" {
				t.Skipf("%s and %s must both be set: name an existing VPC and one of its network interfaces", vpcTestIDEnvVar, vpcNicTestIDEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "gpcn_vpc_public_ip" "test" {
  vpc_id = "` + vpcID + `"
}

resource "gpcn_vpc_public_ip_attachment" "test" {
  vpc_id       = "` + vpcID + `"
  public_ip_id = gpcn_vpc_public_ip.test.id
  nic_id       = "` + nicID + `"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVPCPublicIpAttachmentTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "vpc_id", vpcID),
					resource.TestCheckResourceAttr(gpcnVPCPublicIpAttachmentTest, "nic_id", nicID),
					resource.TestCheckResourceAttrPair(gpcnVPCPublicIpAttachmentTest, "public_ip_id", gpcnVPCPublicIpTest, "id"),
					resource.TestCheckResourceAttrPair(gpcnVPCPublicIpAttachmentTest, "id", gpcnVPCPublicIpTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVPCPublicIpAttachmentTest, "virtual_machine_id"),
				),
			},
		},
	})
}
