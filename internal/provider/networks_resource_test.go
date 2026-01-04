package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestNetworksResource(t *testing.T) {
	gpcnNetworksTest := "gpcn_network.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  description = "An example Network for a demo of Terraform! This one uses the standard network_type."

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"

  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworksTest, "cidr_block", "10.0.0.0/24"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "description", "An example Network for a demo of Terraform! This one uses the standard network_type."),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "dhcp_end_address", "10.0.0.254"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "dhcp_start_address", "10.0.0.10"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "dns_servers", "8.8.8.8, 8.8.4.4"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "name", "terraform-demo-standard"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "network_type", "standard"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "connected_vms"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "gateway"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "snat"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction(gpcnNetworksTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// ImportState testing
			{
				ResourceName: gpcnNetworksTest,
				ImportState:  true,
			},
			// Update and Read testing with little changes
			{
				Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  description = "An example Network for a demo of Terraform! This one uses the standard network_type."

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"

  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworksTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "dhcp_end_address", "10.0.0.140"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "name", "terraform-demo-standard"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "network_type", "standard"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// This is a straightforward update, check for a regular update action
						plancheck.ExpectResourceAction(gpcnNetworksTest, plancheck.ResourceActionUpdate),
					},
				},
			},
			// Update and Read testing with a replace
			{
				Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-custom"

  description = "An example Network for a demo of Terraform! This one uses the custom network_type."

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "custom"
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworksTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "name", "terraform-demo-custom"),
					resource.TestCheckResourceAttr(gpcnNetworksTest, "network_type", "custom"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworksTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Since we are switching to custom, we will need to destroy and re-create
						plancheck.ExpectResourceAction(gpcnNetworksTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestNetworkTypeInvalid(t *testing.T) {
	t.Run("invalid_network_type", func(t *testing.T) {

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Validate network_type value not in standard or custom
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "bad_value"
}
`,
					ExpectError: regexp.MustCompile("Invalid Attribute Value Match"),
				},
			},
		})
	})
}

func TestStandardNetworkValidator(t *testing.T) {
	missingRequiredAttributeErr := "Missing required attribute"

	t.Run("missing_cidr_block", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dhcp_start_address", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dhcp_end_address", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})

	t.Run("missing_dns_servers", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"
}
`,
					ExpectError: regexp.MustCompile(missingRequiredAttributeErr),
				},
			},
		})
	})
}

func TestIpAddressValidator(t *testing.T) {
	t.Run("invalid_dhcp_start_address", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.1455"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile("does not resolve to a valid IPv4 address"),
				},
			},
		})
	})

	t.Run("invalid_dhcp_end_address", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.1405"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile("does not resolve to a valid IPv4 address"),
				},
			},
		})
	})

	t.Run("dhcp_start_address_not_in_cidr", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "192.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile("is not a valid IP address in the CIDR"),
				},
			},
		})
	})
}

func TestCIDRValidator(t *testing.T) {
	t.Run("invalid_cidr_block", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/245"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile("does not contain a valid CIDR block"),
				},
			},
		})
	})

	t.Run("dhcp_address_not_in_cidr_block", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "100.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
					ExpectError: regexp.MustCompile("is not a valid IP address in the CIDR"),
				},
			},
		})
	})
}

func TestDNSServersValidator(t *testing.T) {
	t.Run("invalid_dns_server_ip", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4, 123.123.123.1234"
}
`,
					ExpectError: regexp.MustCompile("is not a valid IPv4 address"),
				},
			},
		})
	})

	t.Run("dns_servers_hanging_comma", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {

  name = "terraform-demo-standard"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  network_type = "standard"

  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"

  dns_servers = "8.8.8.8, 8.8.4.4, "
}
`,
					ExpectError: regexp.MustCompile("is not a valid IPv4 address"),
				},
			},
		})
	})
}
