
# Example: Creating GPCN GPUs
#
# This example demonstrates creating GPU resources by specifying either
# a series name or series code. GPU count must be 1, 2, or 4. A GPU with
# the provided specs is not guaranteed to be available

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

# Lookup datacenter in Central US region
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "central"
  name         = "Kansas"
}

# Provide an existing public key
resource "gpcn_ssh_key" "uploaded" {
  name = "terraform-demo-key-uploaded"
  # Not a real secret
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}

resource "gpcn_gpu" "example" {
  name          = "terraform-demo-gpu"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  # Only one can be specified but one must be
  series_name = "NVIDIA RTX A6000 Series"
  # series_code = "nvidia-rtx_a6000-series"

  # Can only be one of 1,2, or 4
  gpu_count = 1

  # Must be one of "ubuntu-22.04" or "ubuntu-24.04"
  image_name = "ubuntu-22.04"

  # Initial authentication (applied at creation time only; changes update state without affecting the machine)
  initial_auth = {
    ssh_key_id = gpcn_ssh_key.uploaded.id
  }
}
