---
page_title: "Getting started with the GPCN™ provider"
subcategory: ""
description: |-
  Build a VPC, a subnet, a security group and your first virtual machine with the GPCN™ provider.
---

# Getting started

This guide walks you through your first GPCN™ (Global Private Cloud Network™) configuration. You'll set up an API key, then build a VPC, a subnet, a security group and a virtual machine (VM) you can reach over SSH.

By the end you'll have a running VM with an elastic IP. You'll also know how to change it and tear it down.

For a longer walkthrough that also covers volumes and sharing state with colleagues, see [Terraform Provider](https://hub.gpcn.com/how-tos/end-to-end-workflows/terraform-provider) on the GPCN™ Hub.

## Prerequisites

- Terraform 1.5 or later
- A GPCN™ [API key](https://hub.gpcn.com/docs/identity-and-access/credentials/api-keys) whose role can create VMs, VPCs and SSH keys in your organization's Default resource group
- An SSH key pair on your computer (this guide reads `~/.ssh/id_ed25519.pub`)

## Set up your API key

Export the key and the API address in the shell you'll run Terraform from:

```shell
export GPCN_HOST="https://api.gpcn.com"
export GPCN_API_KEY="<your API key>"
```

The provider reads both from the environment, so the `provider "gpcn"` block can stay empty.

## Write the configuration

Create an empty directory with a `main.tf` file, and paste in the configuration below.

```terraform
terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.6.0"
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
```

Before you apply it, change two values:

- Set `remote_cidr` in the `ssh` security group to your own public address, as a `/32`.
- If your public key lives somewhere else, change the path in `gpcn_ssh_key`.

The `ssh` security group's rules are its complete rule set. A new group doesn't carry the rules of the VPC's default group, so keep the rule that allows traffic from the subnet. Without it, SSH to the VM doesn't connect.

The data sources look up the data center, the image and the size, so the configuration never hard-codes an ID. Each resource names the one before it, which tells Terraform the order to build them in.

## Build it

Install the provider, then preview the changes:

```shell
terraform init
terraform plan
```

The plan lists five resources to add. If it looks right, apply it:

```shell
terraform apply
```

The VM takes a few minutes. When the apply finishes, Terraform prints the VM's elastic IP, and you can sign in:

```shell
ssh almalinux@$(terraform output -raw web_public_ip)
```

## Change it

Edit the configuration and run `terraform apply` again. Terraform works out what changed and does only that.

Some changes update the VM in place, such as a new `name`, or a `size_id` the VM can upgrade to. Others replace it, such as a new `image_id` or `subnet_id`. The plan tells you which before anything happens: look for `must be replaced`.

## Tear it down

When you're done, remove everything this configuration built:

```shell
terraform destroy
```

Destroying the VM also releases the elastic IP, because Terraform acquired it with `allocate_public_ip`.

## Next Steps

- [Organizing with resource groups](resource-groups) — put resources in a group other than Default
- [`gpcn_virtualmachine`](../resources/virtualmachine) — every VM setting, including volumes, L2 segments and upgrades
- [`gpcn_vpc_nsg`](../resources/vpc_nsg) — security group rules in detail
- [VPCs on the GPCN™ Hub](https://hub.gpcn.com/docs/resources/networking/vpcs/overview) — how VPCs, subnets and security groups fit together
