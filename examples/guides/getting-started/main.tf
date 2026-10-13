terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.5.0"
    }
  }
}

provider "gpcn" {}

# Look up a data center that offers VPCs
data "gpcn_datacenters" "chicago" {
  name        = "Chicago 1"
  vpc_capable = true
}

locals {
  datacenter_id = data.gpcn_datacenters.chicago.datacenters[0].id
}

# Look up an image and a size offered in that data center
data "gpcn_virtualmachine_images" "alma" {
  datacenter_id = local.datacenter_id
  image_name    = "Alma Linux 8.x"
}

data "gpcn_virtualmachine_sizes" "small" {
  datacenter_id = local.datacenter_id
  category      = "general-purpose"
  min_cpu       = 2
}

# The private network the VM lives on
resource "gpcn_vpc" "main" {
  name          = "getting-started"
  datacenter_id = local.datacenter_id
  cidr          = "10.20.0.0/16"
}

# Allow SSH in from your own address, traffic from inside the subnet,
# and all traffic out. These rules are the group's whole rule set.
resource "gpcn_vpc_nsg" "ssh" {
  vpc_id = gpcn_vpc.main.id
  name   = "ssh"

  rule {
    direction      = "ingress"
    protocol       = "tcp"
    port_range_min = 22
    port_range_max = 22
    remote_cidr    = "203.0.113.10/32" # replace with your public address
    description    = "SSH from my address"
  }

  rule {
    direction   = "ingress"
    protocol    = "all"
    remote_cidr = "10.20.1.0/24"
    description = "All traffic from the app subnet"
  }

  rule {
    direction   = "egress"
    protocol    = "all"
    remote_cidr = "0.0.0.0/0"
    description = "All outbound traffic"
  }
}

resource "gpcn_vpc_subnet" "app" {
  vpc_id = gpcn_vpc.main.id
  name   = "app"
  cidr   = "10.20.1.0/24"
  nsg_id = gpcn_vpc_nsg.ssh.id
}

# The public key you'll sign in with
resource "gpcn_ssh_key" "me" {
  name       = "getting-started"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "gpcn_virtualmachine" "web" {
  name          = "web-1"
  datacenter_id = local.datacenter_id
  size_id       = data.gpcn_virtualmachine_sizes.small.sizes[0].id
  image_id      = data.gpcn_virtualmachine_images.alma.images[0].id
  subnet_id     = gpcn_vpc_subnet.app.id

  # Acquire an elastic IP so you can reach the VM over SSH
  allocate_public_ip = true

  initial_auth = {
    ssh_key_id = gpcn_ssh_key.me.id
    username   = "almalinux"
  }
}

output "web_public_ip" {
  value = gpcn_virtualmachine.web.public_ip
}
