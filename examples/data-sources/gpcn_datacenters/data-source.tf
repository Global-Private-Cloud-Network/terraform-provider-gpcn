# Example: Querying GPCN Datacenters
#
# This example demonstrates how to query available datacenters by location.
# Datacenters are used when creating resources like networks, volumes, and
# virtual machines to specify where they should be deployed.

terraform {
  required_providers {
    gpcn = {
      source = "gpcn.com/dev/gpcn"
    }
  }
}

provider "gpcn" {}

# Example 1: Query datacenters in East US region
data "gpcn_datacenters" "east_us" {
  country_name = "United States"
  region_name  = "east"
}

# Example 2: Query datacenters in West US region
data "gpcn_datacenters" "west_us" {
  country_name = "United States"
  region_name  = "west"
}

# Example 3: Query all datacenters in a country
data "gpcn_datacenters" "all_us" {
  country_name = "United States"
}

# Output the first datacenter ID from East US
output "east_us_datacenter_id" {
  description = "ID of the first datacenter in East US"
  value       = data.gpcn_datacenters.east_us.datacenters[0].id
}

# Output all East US datacenter details
output "east_us_datacenters" {
  description = "All datacenters in East US region"
  value       = data.gpcn_datacenters.east_us.datacenters
}

# Output count of datacenters in West US
output "west_us_datacenter_count" {
  description = "Number of datacenters in West US region"
  value       = length(data.gpcn_datacenters.west_us.datacenters)
}
