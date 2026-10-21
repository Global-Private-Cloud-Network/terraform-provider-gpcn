# Example: Listing GPCN™ resource groups
#
# This example lists the resource groups the API key can see and reads the
# tenant's Default resource group, which new resources join when they name no
# other group.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.6.0"
    }
  }
}

provider "gpcn" {}

data "gpcn_resource_groups" "all" {}

# Look up one group by its exact name
data "gpcn_resource_groups" "platform" {
  name = "platform"
}

output "default_resource_group_id" {
  description = "ID of the tenant's Default resource group"
  value       = data.gpcn_resource_groups.all.default_resource_group_id
}

output "platform_resource_group_id" {
  description = "ID of the platform resource group"
  value       = data.gpcn_resource_groups.platform.resource_groups[0].id
}
