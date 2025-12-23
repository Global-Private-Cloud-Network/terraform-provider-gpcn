
# Example: Creating GPCN Networks
#
# This example demonstrates creating both standard and custom network types.
# Standard networks include DHCP, DNS, and SNAT configuration.
# Custom networks provide more flexibility for advanced networking setups.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.1.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Lookup datacenter in East US region
data "gpcn_datacenters" "east_us" {
  country_name = "United States"
  region_name  = "east"
}

# Example 1: Standard Network with DHCP and DNS
resource "gpcn_network" "example_standard" {
  name          = "terraform-demo-standard"
  network_type  = "standard"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  description = "Standard network for web application VMs"

  # Network configuration
  cidr_block = "10.0.0.0/24"

  # DHCP range (both required together)
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"

  # DNS servers
  dns_servers = "8.8.8.8"
}

output "gpcn_network_example_standard" {
  value = gpcn_network.example_standard
}

# Example 2: Custom Network
resource "gpcn_network" "example_custom" {
  name          = "terraform-demo-custom"
  network_type  = "custom"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  description = "Custom network for advanced networking configuration"
}

output "gpcn_network_example_custom" {
  value = gpcn_network.example_custom
}
