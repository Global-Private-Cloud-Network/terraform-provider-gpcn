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

resource "gpcn_volume" "vm_storage_primary" {
  name          = "vm-storage-primary"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

resource "gpcn_volume" "vm_storage_secondary" {
  name          = "vm-storage-secondary"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type   = "SSD"
  size_gb       = 256
}

resource "gpcn_virtualmachine" "example" {
  name          = "terraform-demo-vm"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  size_id  = data.gpcn_virtualmachine_sizes.micro.sizes[0].id
  image_id = data.gpcn_virtualmachine_images.alma_8.images[0].id

  allocate_public_ip = false

  initial_auth = {
    username = "almalinux"
    password = "SamplePassword123!"
  }
}

resource "gpcn_volume_attachment" "primary_storage" {
  virtual_machine_id = gpcn_virtualmachine.example.id
  volume_id          = gpcn_volume.vm_storage_primary.id
}

# When attaching multiple volumes to a VM with network_hotplug=false,
# use depends_on to serialize the operations and avoid concurrent stop/start races
resource "gpcn_volume_attachment" "secondary_storage" {
  virtual_machine_id = gpcn_virtualmachine.example.id
  volume_id          = gpcn_volume.vm_storage_secondary.id
  depends_on         = [gpcn_volume_attachment.primary_storage]
}
