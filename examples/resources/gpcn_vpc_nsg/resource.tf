# Example: A network security group for a GPCN VPC
#
# The rules live inline. GPCN replaces the whole rule set on every change and
# identifies a rule by its direction, protocol, port range and remote CIDR, so a
# rule has no identity of its own to manage separately.

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

# Lookup datacenter in Central US region
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago"
}

resource "gpcn_vpc" "example" {
  name          = "terraform-demo-vpc"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  cidr          = "10.60.0.0/16"
}

resource "gpcn_vpc_nsg" "example" {
  vpc_id      = gpcn_vpc.example.id
  name        = "terraform-demo-web"
  description = "Web tier posture"

  rule {
    direction      = "ingress"
    protocol       = "tcp"
    port_range_min = 443
    port_range_max = 443
    remote_cidr    = "0.0.0.0/0"
    description    = "HTTPS from anywhere"
  }

  # Ports apply to tcp and udp only. Omit them for icmp and all.
  rule {
    direction   = "ingress"
    protocol    = "icmp"
    remote_cidr = "10.60.0.0/16"
    description = "Ping from inside the VPC"
  }

  rule {
    direction   = "egress"
    protocol    = "all"
    remote_cidr = "0.0.0.0/0"
    description = "All outbound traffic"
  }
}

# Bind a subnet to the group
resource "gpcn_vpc_subnet" "example" {
  vpc_id = gpcn_vpc.example.id
  name   = "terraform-demo-web"
  cidr   = "10.60.1.0/24"
  nsg_id = gpcn_vpc_nsg.example.id
}

output "gpcn_vpc_nsg_example" {
  value = gpcn_vpc_nsg.example
}
