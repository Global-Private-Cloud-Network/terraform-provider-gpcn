# Example: Querying GPCN Datacenters
#
# This example demonstrates how to query available datacenters by location.
# Datacenters are used when creating resources like networks, volumes, and
# virtual machines to specify where they should be deployed.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.5.4"
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

# Example 4: Query datacenters that support GPUs
data "gpcn_datacenters" "gpu_capable" {
  gpu_enabled = true
}

# Example 5: Query GPU-capable datacenters in a specific region
data "gpcn_datacenters" "gpu_capable_east_us" {
  country_name = "United States"
  region_name  = "east"
  gpu_enabled  = true
}

# Example 6: Query datacenters that support custom images
data "gpcn_datacenters" "custom_image_capable" {
  custom_images = true
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

# Output all GPU-capable datacenter IDs
output "gpu_capable_datacenter_ids" {
  description = "IDs of all datacenters with GPU support"
  value       = [for dc in data.gpcn_datacenters.gpu_capable.datacenters : dc.id]
}

# Output all datacenters supporting custom images
output "custom_image_datacenter_ids" {
  description = "IDs of all datacenters that support custom images"
  value       = [for dc in data.gpcn_datacenters.custom_image_capable.datacenters : dc.id]
}
