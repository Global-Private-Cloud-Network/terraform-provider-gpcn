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

// This case owns the second octet after the gateway case: 10.121.0.0/16.
const vpnConnAccOctet = vpnGatewayAccOctetBase + 1

// The guard refuses an octet outside the VPN window or one the gateway case
// already takes.
func TestVpcVpnConnectionAcceptanceOctetIsUnique(t *testing.T) {
	if vpnConnAccOctet < vpnGatewayAccOctetBase || vpnConnAccOctet >= vpnGatewayAccOctetBase+vpnGatewayAccOctetSlots {
		t.Errorf("Expected octet %d inside the window at %d", vpnConnAccOctet, vpnGatewayAccOctetBase)
	}
	if vpnConnAccOctet == vpnGatewayAccOctetFor(t, "TestVpcVpnGatewayResource") {
		t.Errorf("Expected a different octet from the gateway case, both take %d", vpnConnAccOctet)
	}
}

// vpnConnAccTestConfig builds a VPC, one subnet, the VPN gateway and a VPN
// connection with a write-only key. 192.0.2.0/24 and 198.51.100.0/24 are
// documentation ranges, so no real peer answers.
func vpnConnAccTestConfig(datacenterID, suffix, vpcCidr, subnetCidr, connName, peerCidrs string, pskVersion int) string {
	return providerConfig + fmt.Sprintf(`
resource "gpcn_vpc" "test" {
  name                = "tf-vpnc-vpc-%[1]s"
  datacenter_id       = %[2]q
  cidr                = %[3]q
  acknowledge_overlap = true
}

resource "gpcn_vpc_subnet" "test" {
  vpc_id = gpcn_vpc.test.id
  name   = "tf-vpnc-subnet-%[1]s"
  cidr   = %[4]q
}

resource "gpcn_vpc_vpn_gateway" "test" {
  vpc_id     = gpcn_vpc.test.id
  depends_on = [gpcn_vpc_subnet.test]
}

resource "gpcn_vpc_vpn_connection" "test" {
  vpc_id         = gpcn_vpc.test.id
  vpn_gateway_id = gpcn_vpc_vpn_gateway.test.id
  name           = %[5]q
  peer_address   = "203.0.113.10"
  peer_cidrs     = %[6]s
  psk_wo         = "tf-acc-pre-shared-key-%[1]s"
  psk_wo_version = %[7]d
}
`, suffix, datacenterID, vpcCidr, subnetCidr, connName, peerCidrs, pskVersion)
}

func TestVpcVpnConnectionResource(t *testing.T) {
	t.Parallel()
	datacenterID := os.Getenv(vpnGatewayAccDatacenterEnvVar)

	suffix := acctest.RandString(8)
	vpcCidr := fmt.Sprintf("10.%d.0.0/16", vpnConnAccOctet)
	subnetCidr := fmt.Sprintf("10.%d.1.0/24", vpnConnAccOctet)
	first := vpnConnAccTestConfig(datacenterID, suffix, vpcCidr, subnetCidr, "tf-acc-branch-"+suffix, `["192.168.10.0/24"]`, 1)
	second := vpnConnAccTestConfig(datacenterID, suffix, vpcCidr, subnetCidr, "tf-acc-branch-renamed-"+suffix, `["192.168.10.0/24", "192.168.20.0/24"]`, 2)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if datacenterID == "" {
				t.Skipf("%s is not set: name a data center that offers site-to-site VPN", vpnGatewayAccDatacenterEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: first,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVpcVpnConnectionTest, "id"),
					resource.TestCheckResourceAttrPair(gpcnVpcVpnConnectionTest, "vpc_id", "gpcn_vpc.test", "id"),
					resource.TestCheckResourceAttrPair(gpcnVpcVpnConnectionTest, "vpn_gateway_id", "gpcn_vpc_vpn_gateway.test", "id"),
					resource.TestCheckResourceAttrSet(gpcnVpcVpnConnectionTest, "state"),
					resource.TestCheckResourceAttrSet(gpcnVpcVpnConnectionTest, "gateway_kind"),
					resource.TestCheckResourceAttrSet(gpcnVpcVpnConnectionTest, "local_endpoint_address"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "ike_version", "v2"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "dh_group", "14"),
					resource.TestCheckNoResourceAttr(gpcnVpcVpnConnectionTest, "psk_wo"),
				),
			},
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVpcVpnConnectionTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "name", "tf-acc-branch-renamed-"+suffix),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "peer_cidrs.#", "2"),
					resource.TestCheckResourceAttr(gpcnVpcVpnConnectionTest, "psk_wo_version", "2"),
				),
			},
			{
				Config:                  second,
				ResourceName:            gpcnVpcVpnConnectionTest,
				ImportState:             true,
				ImportStateIdFunc:       vpnConnAccImportID,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"psk_wo_version"},
			},
		},
	})
}

// The import ID joins the VPC ID and the connection ID with a slash.
func vpnConnAccImportID(state *terraform.State) (string, error) {
	connection, ok := state.RootModule().Resources[gpcnVpcVpnConnectionTest]
	if !ok {
		return "", fmt.Errorf("resource %s is not in the state", gpcnVpcVpnConnectionTest)
	}
	return connection.Primary.Attributes["vpc_id"] + "/" + connection.Primary.ID, nil
}
