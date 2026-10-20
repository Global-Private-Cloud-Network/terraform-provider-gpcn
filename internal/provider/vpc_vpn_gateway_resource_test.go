package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// vpnGatewayAccDatacenterEnvVar names a data center that offers site-to-site
// VPN. The case takes it from the environment, because only the platform
// decides VPN capability. Run it once per gateway kind.
const vpnGatewayAccDatacenterEnvVar = "GPCN_TEST_VPN_DATACENTER_ID"

// This file owns the window at base 120. The VPC, subnet, group and machine
// files own 64, 80, 96 and 112.
const (
	vpnGatewayAccOctetBase  = 120
	vpnGatewayAccOctetSlots = 8
)

var vpnGatewayAccOctetSlot = map[string]int{
	"TestVpcVpnGatewayResource": 0,
}

func vpnGatewayAccOctetFor(t testing.TB, name string) int {
	t.Helper()
	slot, named := vpnGatewayAccOctetSlot[name]
	if !named {
		t.Fatalf("Expected %s to own a slot in vpnGatewayAccOctetSlot", name)
	}
	return vpnGatewayAccOctetBase + slot
}

// The guard reads the table alone. It refuses a slot outside this file's
// window and two entries that take one octet.
func TestVpcVpnGatewayAcceptanceOctetsAreUnique(t *testing.T) {
	owner := map[int]string{}
	for name, slot := range vpnGatewayAccOctetSlot {
		if slot < 0 || slot >= vpnGatewayAccOctetSlots {
			t.Errorf("Expected %s to take a slot below %d, got %d", name, vpnGatewayAccOctetSlots, slot)
		}
		octet := vpnGatewayAccOctetFor(t, name)
		if other, taken := owner[octet]; taken {
			t.Errorf("Expected %s and %s to take different octets, both take %d", name, other, octet)
		}
		owner[octet] = name
	}
}

// vpnGatewayAccTestConfig builds a VPC, one subnet and the VPN gateway. The
// appliance kind carves its block from the VPC, so the VPC needs a subnet and
// free space.
func vpnGatewayAccTestConfig(datacenterID, vpcName, vpcCidr, subnetName, subnetCidr string) string {
	return providerConfig + fmt.Sprintf(`
resource "gpcn_vpc" "test" {
  name                = %q
  datacenter_id       = %q
  cidr                = %q
  acknowledge_overlap = true
}

resource "gpcn_vpc_subnet" "test" {
  vpc_id = gpcn_vpc.test.id
  name   = %q
  cidr   = %q
}

resource "gpcn_vpc_vpn_gateway" "test" {
  vpc_id     = gpcn_vpc.test.id
  depends_on = [gpcn_vpc_subnet.test]
}
`, vpcName, datacenterID, vpcCidr, subnetName, subnetCidr)
}

func TestVpcVpnGatewayResource(t *testing.T) {
	t.Parallel()
	datacenterID := os.Getenv(vpnGatewayAccDatacenterEnvVar)

	suffix := acctest.RandString(8)
	vpcName := fmt.Sprintf("tf-vpn-vpc-%s", suffix)
	subnetName := fmt.Sprintf("tf-vpn-subnet-%s", suffix)
	octet := vpnGatewayAccOctetFor(t, t.Name())
	vpcCidr := fmt.Sprintf("10.%d.0.0/16", octet)
	subnetCidr := fmt.Sprintf("10.%d.1.0/24", octet)
	config := vpnGatewayAccTestConfig(datacenterID, vpcName, vpcCidr, subnetName, subnetCidr)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if datacenterID == "" {
				t.Skipf("%s is not set: name a data center that offers site-to-site VPN", vpnGatewayAccDatacenterEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnGatewayTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVpcVpnGatewayTest, "id"),
					resource.TestCheckResourceAttrPair(gpcnVpcVpnGatewayTest, "vpc_id", "gpcn_vpc.test", "id"),
					resource.TestCheckResourceAttr(gpcnVpcVpnGatewayTest, "state", "ready"),
					resource.TestCheckResourceAttrSet(gpcnVpcVpnGatewayTest, "kind"),
					resource.TestCheckResourceAttrSet(gpcnVpcVpnGatewayTest, "address"),
				),
			},
			{
				Config:            config,
				ResourceName:      gpcnVpcVpnGatewayTest,
				ImportState:       true,
				ImportStateIdFunc: vpnGatewayAccImportID,
				ImportStateVerify: true,
			},
		},
	})
}

// The import ID is the ID of the VPC, because a VPC has one VPN gateway.
func vpnGatewayAccImportID(state *terraform.State) (string, error) {
	gateway, ok := state.RootModule().Resources[gpcnVpcVpnGatewayTest]
	if !ok {
		return "", fmt.Errorf("resource %s is not in the state", gpcnVpcVpnGatewayTest)
	}
	return gateway.Primary.Attributes["vpc_id"], nil
}
