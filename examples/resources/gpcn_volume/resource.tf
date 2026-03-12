
# Example: Creating GPCN Volumes
#
# This example demonstrates creating storage volumes with different types.
# Volumes can be attached to virtual machines for additional storage capacity.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.4.2"
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

# SSD Volume
resource "gpcn_volume" "example_ssd" {
  name          = "terraform-demo-ssd"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

output "example_gpcn_volume_ssd" {
  value = gpcn_volume.example_ssd
}
