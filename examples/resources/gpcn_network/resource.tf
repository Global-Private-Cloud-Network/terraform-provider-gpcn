
# gpcn_network is deprecated in favour of gpcn_vpc, gpcn_vpc_subnet and gpcn_l2_segment.
#
# Example: Reading and updating a GPCN Network
#
# New networks can no longer be created. This example shows the shape of a network that
# already exists, which Terraform reaches with terraform import.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.4.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Lookup datacenter in Central US region
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago"
}

# A standard network with DHCP and DNS
resource "gpcn_network" "example_standard" {
  name          = "terraform-demo-standard"
  network_type  = "standard"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  description = "Standard network for web application VMs"

  # Network configuration
  cidr_block = "10.0.0.0/24"

  # Optional: default route (gateway). Defaults to the first usable host of cidr_block.
  # gateway_ip = "10.0.0.1"

  # DHCP range (both required together)
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"

  # DNS servers
  dns_servers = ["8.8.8.8"]
}

output "gpcn_network_example_standard" {
  value = gpcn_network.example_standard
}
