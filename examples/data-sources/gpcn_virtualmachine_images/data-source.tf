terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>1.0.2"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Lookup a datacenter first
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago"
}

# List all Linux images for a datacenter
data "gpcn_virtualmachine_images" "linux_images" {
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  image_type    = "Linux"
}

# Find a specific image by name (substring match, case-insensitive)
data "gpcn_virtualmachine_images" "alma_8" {
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  image_name    = "Alma Linux 8"
}

# Look up a general-purpose size with at least 2 CPU cores
data "gpcn_virtualmachine_sizes" "micro" {
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  category      = "general-purpose"
  min_cpu       = 2
}

# Use the image ID and size ID when creating a virtual machine
resource "gpcn_virtualmachine" "example" {
  name          = "terraform-demo-vm"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  size_id  = data.gpcn_virtualmachine_sizes.micro.sizes[0].id
  image_id = data.gpcn_virtualmachine_images.alma_8.images[0].id

  allocate_public_ip = false

  initial_auth = {
    username   = "almalinux"
    ssh_key_id = "your-ssh-key-id"
  }
}
