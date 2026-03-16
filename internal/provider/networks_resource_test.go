package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnNetworkTest = "gpcn_network.test"

func TestNetworksResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  description = "An example Network for a demo of Terraform! This one uses the standard network_type."
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  network_type = "standard"
  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"
  dns_servers = "8.8.8.8, 8.8.4.4"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworkTest, "cidr_block", "10.0.0.0/24"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "description", "An example Network for a demo of Terraform! This one uses the standard network_type."),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "dhcp_end_address", "10.0.0.254"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "dhcp_start_address", "10.0.0.10"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "dns_servers", "8.8.8.8, 8.8.4.4"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "terraform-demo-standard"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "network_type", "standard"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "connected_vms"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "gateway"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "snat"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// ImportState testing
			{
				ResourceName: gpcnNetworkTest,
				ImportState:  true,
			},
			// Update and Read testing with little changes
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  description = "An example Network for a demo of Terraform! This one uses the standard network_type."
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  network_type = "standard"
  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.140"
  dns_servers = "8.8.8.8, 8.8.4.4"
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworkTest, "dhcp_end_address", "10.0.0.140"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "terraform-demo-standard"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "network_type", "standard"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// This is a straightforward update, check for a regular update action
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnNetworkTest, tfjsonpath.New("network_type"), knownvalue.StringExact("standard")),
					statecheck.ExpectKnownValue(gpcnNetworkTest, tfjsonpath.New("dhcp_end_address"), knownvalue.StringExact("10.0.0.140")),
				},
			},
			// Update and Read testing with a replace
			// Changing network_type forces a replace
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_network" "test" {
  name = "terraform-demo-custom"
  description = "An example Network for a demo of Terraform! This one uses the custom network_type."
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  network_type = "custom"
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnNetworkTest, "name", "terraform-demo-custom"),
					resource.TestCheckResourceAttr(gpcnNetworkTest, "network_type", "custom"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnNetworkTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Since we are switching to custom, we will need to destroy and re-create
						plancheck.ExpectResourceAction(gpcnNetworkTest, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnNetworkTest, tfjsonpath.New("network_type"), knownvalue.StringExact("custom")),
				},
			},
		},
	})
}

/*
*
----- Unit tests -----
*
*/
func TestNetworksResourceInvalidType(t *testing.T) {
	t.Run("invalid_network_type", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Validate network_type value not in standard or custom
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  datacenter_id = "any-datacenter-id"
  network_type = "bad_value"
}
`,
					ExpectError: regexp.MustCompile("Invalid Attribute Value Match"),
				},
			},
		})
	})
}

func TestNetworksResourceStandardValidator(t *testing.T) {
	missingRequiredAttributeErr := "Missing required attribute"

	t.Run("missing_cidr_block", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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

func TestNetworksResourceIpAddressValidator(t *testing.T) {
	t.Run("invalid_dhcp_start_address", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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

func TestNetworksResourceCIDRValidator(t *testing.T) {
	t.Run("invalid_cidr_block", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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

func TestNetworksResourceDNSServersValidator(t *testing.T) {
	t.Run("invalid_dns_server_ip", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_network" "test" {
  name = "terraform-demo-standard"
  datacenter_id = "any-datacenter-id"
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
  datacenter_id = "any-datacenter-id"
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
