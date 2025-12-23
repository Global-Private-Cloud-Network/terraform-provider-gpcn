
# Example: Creating GPCN Virtual Machines
#
# This example demonstrates creating a virtual machine with networks and volumes.
# It shows how to create the required dependencies (networks and volumes) and
# attach them to a virtual machine instance.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.1.0"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Lookup datacenter in East US region
data "gpcn_datacenters" "east_us" {
  country_name = "United States"
  region_name  = "east"
}

# Create a standard network for the VM
resource "gpcn_network" "vm_network" {
  name          = "vm-network-standard"
  network_type  = "standard"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  description = "Standard network for virtual machine connectivity"

  # Network configuration
  cidr_block = "10.0.0.0/24"

  # DHCP range
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"

  # DNS servers
  dns_servers = "8.8.8.8, 8.8.4.4"
}

# Create a custom network for additional connectivity
resource "gpcn_network" "vm_network_custom" {
  name          = "vm-network-custom"
  network_type  = "custom"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  description = "Custom network for advanced networking configuration"
}

# Create storage volumes for the VM
resource "gpcn_volume" "vm_storage" {
  name          = "vm-storage-primary"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

# Create the virtual machine
resource "gpcn_virtualmachine" "example" {
  name          = "terraform-demo-vm"
  datacenter_id = data.gpcn_datacenters.east_us.datacenters[0].id

  # Compute configuration
  size  = "Micro"
  image = "Alma Linux 8.x"

  wait_for_startup = false

  # Networking
  allocate_public_ip = false
  network_ids = [
    gpcn_network.vm_network.id,
    gpcn_network.vm_network_custom.id
  ]

  # Storage
  volume_ids = [
    gpcn_volume.vm_storage.id
  ]
}

output "example_gpcn_virtualmachine" {
  value = gpcn_virtualmachine.example
}
