# Example: Acquiring a GPCN VPC elastic public IP address

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.4.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# The VPC the address is acquired from: a gpcn_vpc resource.
variable "vpc_id" {
  type = string
}

# The address is held by the VPC until an attachment binds it to an interface.
resource "gpcn_vpc_public_ip" "example" {
  vpc_id = var.vpc_id
}

output "public_ip_address" {
  value = gpcn_vpc_public_ip.example.ip_address
}
