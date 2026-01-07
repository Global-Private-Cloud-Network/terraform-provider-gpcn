
# Example: Creating GPCN GPUs
#
# This example demonstrates creating GPU resources by specifying either
# a series name or series code. GPU count must be 1, 2, or 4.

terraform {
  required_providers {
    gpcn = {
      source = "gpcn.com/dev/gpcn"
    }
  }
}

provider "gpcn" {
  #   host = "https://api.gpcn.com"
}

# Lookup datacenter in East US region
data "gpcn_datacenters" "east_us" {
  country_name = "United States"
  region_name  = "east"
}

resource "gpcn_gpu" "example" {
  name          = "terraform-demo-gpu"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  # Only one can be specified but one must be
  #   series_name = "H100 Series"
  series_code = "h100_series"

  # Can only be one of 1,2,4
  gpu_count = 1
}
