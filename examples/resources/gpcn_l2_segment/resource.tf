# Example: Managing a GPCN L2 segment
#
# An L2 segment is a layer-2 network inside one datacenter. It carries no addressing,
# so there is no CIDR block, no gateway and no DHCP range to configure.

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

resource "gpcn_l2_segment" "example" {
  name          = "terraform-demo-segment"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  description = "Layer-2 segment for east-west application traffic"
}

output "gpcn_l2_segment_example" {
  value = gpcn_l2_segment.example
}
