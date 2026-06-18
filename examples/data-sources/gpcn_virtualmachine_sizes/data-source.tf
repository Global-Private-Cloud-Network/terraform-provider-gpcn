terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.0.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Lookup a datacenter first
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago"
}

# List all general-purpose sizes with at least 4 CPU cores and 8 GB RAM
data "gpcn_virtualmachine_sizes" "general_medium_plus" {
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  category      = "general-purpose"
  min_cpu       = 4
  min_memory_gb = 8
}

# Use the first matching size in a virtual machine resource
# resource "gpcn_virtualmachine" "example" {
#   size_id = data.gpcn_virtualmachine_sizes.general_medium_plus.sizes[0].id
#   ...
# }
