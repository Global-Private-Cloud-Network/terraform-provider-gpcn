terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.6.0"
    }
  }
}

# The provider reads GPCN_HOST and GPCN_API_KEY from the environment,
# so the block can stay empty.
provider "gpcn" {}

# Look up a data center to build in
data "gpcn_datacenters" "chicago" {
  name        = "Chicago 1"
  vpc_capable = true
}

resource "gpcn_vpc" "main" {
  name          = "main"
  datacenter_id = data.gpcn_datacenters.chicago.datacenters[0].id
  cidr          = "10.10.0.0/16"
}
