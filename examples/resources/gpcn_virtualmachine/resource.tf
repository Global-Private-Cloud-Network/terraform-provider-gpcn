
# Example: Creating GPCN Virtual Machines
#
# This example demonstrates creating a virtual machine with networks and volumes.
# It shows how to create the required dependencies (networks and volumes) and
# attach them to a virtual machine instance.

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

# Lookup datacenter in Central US region
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name         = "Chicago"
}

# Look up the image ID for Alma Linux 8
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

# Provide an existing public key
resource "gpcn_ssh_key" "uploaded" {
  name = "terraform-demo-key-uploaded"
  # Not a real secret
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}

# Create a resource group
resource "gpcn_resource_group" "group_example" {
  name = "terraform-demo-group"
}

# Create a standard network for the VM
resource "gpcn_network" "vm_network" {
  name          = "vm-network-standard"
  network_type  = "standard"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  description = "Standard network for virtual machine connectivity"

  # Network configuration
  cidr_block = "10.0.0.0/24"

  # DHCP range
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"

  # DNS servers
  dns_servers = ["8.8.8.8", "8.8.4.4"]
}

# Create a custom network for additional connectivity
resource "gpcn_network" "vm_network_custom" {
  name          = "vm-network-custom"
  network_type  = "custom"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  description = "Custom network for advanced networking configuration"
}

# Create storage volume for the VM
resource "gpcn_volume" "vm_storage" {
  name          = "vm-storage-primary"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

# Create the virtual machine
resource "gpcn_virtualmachine" "example" {
  name          = "terraform-demo-vm"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  # Compute configuration
  size_id  = data.gpcn_virtualmachine_sizes.micro.sizes[0].id
  image_id = data.gpcn_virtualmachine_images.alma_8.images[0].id

  # Networking
  allocate_public_ip = false
  network_ids = [
    gpcn_network.vm_network.id,
    gpcn_network.vm_network_custom.id
  ]

  # Resource Group
  resource_group_id = gpcn_resource_group.group_example.id

  # Initial authentication (applied at creation time only; changes update state without affecting the machine)
  initial_auth = {
    ssh_key_id = gpcn_ssh_key.uploaded.id
    username   = "almalinux"
  }
}

# Attach the storage volume to the virtual machine
resource "gpcn_volume_attachment" "vm_storage_attachment" {
  virtual_machine_id = gpcn_virtualmachine.example.id
  volume_id          = gpcn_volume.vm_storage.id
}
