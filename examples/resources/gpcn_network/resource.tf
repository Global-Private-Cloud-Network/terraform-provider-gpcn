# gpcn_network is deprecated in favour of gpcn_vpc, gpcn_vpc_subnet and gpcn_l2_segment.
#
# Example: Adopting an existing GPCN Network into Terraform
#
# New networks can no longer be created, so this example never declares one. It brings a
# network that already exists under Terraform with an import block, and then reads,
# renames and destroys it.

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

# The id of the network that GPCN already serves.
import {
  to = gpcn_network.existing
  id = "<network-id>"
}

# The block the import fills. Every value must match what GPCN reports for the network,
# or the first plan proposes a change.
resource "gpcn_network" "existing" {
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

output "gpcn_network_existing" {
  value = gpcn_network.existing
}
