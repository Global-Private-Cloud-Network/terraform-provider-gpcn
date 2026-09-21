# Example: Carving subnets out of a GPCN VPC
#
# A subnet takes its datacenter and its address space from its VPC. Give it a
# CIDR inside the VPC super-CIDR, or a prefix length and let GPCN carve a free
# block of that size.

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
  cidr          = "10.60.0.0/16"
}

# A subnet with an explicit CIDR inside the VPC super-CIDR
resource "gpcn_vpc_subnet" "example_explicit" {
  vpc_id      = gpcn_vpc.example.id
  name        = "terraform-demo-web"
  description = "Web tier"
  cidr        = "10.60.1.0/24"
}

# A subnet GPCN carves: ask for a size and let the allocator pick the block
resource "gpcn_vpc_subnet" "example_carved" {
  vpc_id = gpcn_vpc.example.id
  name   = "terraform-demo-data"
  prefix = 26
}

output "gpcn_vpc_subnet_example_explicit" {
  value = gpcn_vpc_subnet.example_explicit
}
