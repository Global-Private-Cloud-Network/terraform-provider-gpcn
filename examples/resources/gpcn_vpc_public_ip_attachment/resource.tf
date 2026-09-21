# Example: Attaching a GPCN VPC elastic public IP address to an interface

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

variable "vpc_id" {
  type = string
}

# The VPC network interface the address serves. A virtual machine's interfaces
# are listed in its network_interfaces attribute.
variable "nic_id" {
  type = string
}

resource "gpcn_vpc_public_ip" "example" {
  vpc_id = var.vpc_id
}

# Destroying the attachment detaches the address and leaves it held, so the
# gpcn_vpc_public_ip resource above survives and can serve another interface.
resource "gpcn_vpc_public_ip_attachment" "example" {
  vpc_id       = var.vpc_id
  public_ip_id = gpcn_vpc_public_ip.example.id
  nic_id       = var.nic_id
}
