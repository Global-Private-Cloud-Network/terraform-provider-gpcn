# Example: Creating a GPCN VPC
#
# A VPC is the routed private network that holds subnets, security groups and
# public IP addresses in one datacenter. The API key needs vpc:read, vpc:create,
# vpc:update and vpc:delete.

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

resource "gpcn_vpc" "example" {
  name          = "terraform-demo-vpc"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  description = "Routed network for the demo application"

  # Super-CIDR the subnets are allocated from. RFC1918, /16 to /24, and at its
  # network address. Changing it replaces the VPC.
  cidr = "10.50.0.0/16"

  # Optional: one or two DNS servers for the guests. The platform chooses them
  # when the list is omitted. Changing them replaces the VPC.
  # dns_nameservers = ["8.8.8.8", "1.1.1.1"]

  # Optional: create the VPC even though its CIDR overlaps another VPC.
  # Overlapping VPCs can never be connected to each other.
  # acknowledge_overlap = true
}

output "gpcn_vpc_example" {
  value = gpcn_vpc.example
}
