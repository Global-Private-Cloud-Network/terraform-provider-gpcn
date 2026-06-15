
# Example: Creating GPCN Resource Groups

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

resource "gpcn_resource_group" "example" {
  name        = "terraform-demo-group-update"
  description = "Example resource group for a demo with terraform"
}
