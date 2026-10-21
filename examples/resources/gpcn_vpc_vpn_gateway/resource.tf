# Example: A VPN gateway for a GPCN™ VPC
#
# A VPN gateway holds the VPN gateway address, the public address your peer
# gateway connects to for site-to-site VPN. A VPC has one. Your organization
# needs the Site-to-Site VPN feature, and the data center must offer
# site-to-site VPN.

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

# The gateway waits for the subnet, because an appliance gateway reserves a
# /28 block inside the VPC CIDR.
resource "gpcn_vpc_vpn_gateway" "example" {
  vpc_id     = gpcn_vpc.example.id
  depends_on = [gpcn_vpc_subnet.example]
}

# Give this address to the administrator of your peer gateway
output "gpcn_vpc_vpn_gateway_address" {
  value = gpcn_vpc_vpn_gateway.example.address
}
