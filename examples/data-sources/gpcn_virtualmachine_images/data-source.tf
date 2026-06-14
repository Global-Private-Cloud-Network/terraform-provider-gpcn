terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.5.4"
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

# Use the image ID when creating a virtual machine
resource "gpcn_virtualmachine" "example" {
  name          = "terraform-demo-vm"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  size = {
    category = "general-purpose"
    name     = "G-Micro-1"
  }

  image_id = data.gpcn_virtualmachine_images.alma_8.images[0].id

  allocate_public_ip = false

  initial_auth = {
    username   = "almalinux"
    ssh_key_id = "your-ssh-key-id"
  }
}
