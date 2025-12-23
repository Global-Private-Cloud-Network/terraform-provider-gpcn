
# Example: Creating GPCN Volumes
#
# This example demonstrates creating storage volumes with different types.
# Volumes can be attached to virtual machines for additional storage capacity.

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

# SSD Volume
resource "gpcn_volume" "example_ssd" {
  name          = "terraform-demo-ssd"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

output "example_gpcn_volume_ssd" {
  value = gpcn_volume.example_ssd
}
