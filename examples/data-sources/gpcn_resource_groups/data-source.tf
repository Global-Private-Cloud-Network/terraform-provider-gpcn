# Example: Listing GPCN resource groups
#
# This example lists the resource groups the API key can see and reads the
# tenant's Default resource group, which new resources join when they name no
# other group.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.5.0"
    }
  }
}

provider "gpcn" {}

data "gpcn_resource_groups" "all" {}

# Look up one group by its exact name
data "gpcn_resource_groups" "team" {
  name = "platform-team"
}

output "default_resource_group_id" {
  description = "ID of the tenant's Default resource group"
  value       = data.gpcn_resource_groups.all.default_resource_group_id
}

output "team_resource_group_id" {
  description = "ID of the platform-team resource group"
  value       = data.gpcn_resource_groups.team.resource_groups[0].id
}
