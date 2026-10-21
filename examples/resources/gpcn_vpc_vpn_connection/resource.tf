# Example: A site-to-site VPN connection for a GPCN™ VPC
#
# A VPN connection joins the VPN gateway of a VPC to a peer gateway at your
# site. Create the VPN gateway first. Your organization needs the Site-to-Site
# VPN feature, and the data center must offer site-to-site VPN.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.6.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# The pre-shared key, 16 to 64 printable ASCII characters. A write-only
# attribute needs Terraform 1.11 or later.
variable "vpn_psk" {
  type      = string
  sensitive = true
}

# Raise this number to send the current key to GPCN™ again.
variable "vpn_psk_version" {
  type    = number
  default = 1
}

# Lookup data center in Central US region
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago 1"
}

resource "gpcn_vpc" "example" {
  name          = "terraform-demo-vpc"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  cidr          = "10.60.0.0/16"
}

resource "gpcn_vpc_subnet" "example" {
  vpc_id = gpcn_vpc.example.id
  name   = "terraform-demo-web"
  cidr   = "10.60.1.0/24"
}

resource "gpcn_vpc_vpn_gateway" "example" {
  vpc_id     = gpcn_vpc.example.id
  depends_on = [gpcn_vpc_subnet.example]
}

resource "gpcn_vpc_vpn_connection" "example" {
  vpc_id         = gpcn_vpc.example.id
  vpn_gateway_id = gpcn_vpc_vpn_gateway.example.id
  name           = "terraform-demo-branch-office"

  # The public address of the peer gateway, and the networks behind it
  peer_address = "203.0.113.10"
  peer_cidrs   = ["192.168.10.0/24"]

  # Terraform never stores a write-only key in the plan or the state
  psk_wo         = var.vpn_psk
  psk_wo_version = var.vpn_psk_version

  # Instead of the two lines above, psk stores the key in state, marked
  # sensitive:
  # psk = var.vpn_psk
}

# Give this address to the administrator of your peer gateway
output "gpcn_vpc_vpn_connection_local_endpoint_address" {
  value = gpcn_vpc_vpn_connection.example.local_endpoint_address
}

# Configure these networks at your peer gateway
output "gpcn_vpc_vpn_connection_local_networks" {
  value = gpcn_vpc_vpn_connection.example.local_networks
}
