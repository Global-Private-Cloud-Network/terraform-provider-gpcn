# Example: Querying GPCN GPU inventory
#
# This example demonstrates how to list the available GPU SKUs for a datacenter.
# Each entry exposes a sku_code that can be passed to a gpcn_gpu resource to pin
# an exact SKU within the series.

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

# Lookup a GPU-enabled datacenter first
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "central"
  name         = "Kansas"
  gpu_enabled  = true
}

# List the available A6000 SKUs with a GPU count of 1
data "gpcn_gpu_inventory" "a6000" {
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  # Only one can be specified but one must be
  # series_name = "NVIDIA RTX A6000 Series"
  series_code = "nvidia-rtx_a6000-series"

  gpu_count = 1
}

# All available SKU codes for that series and count
output "a6000_sku_codes" {
  description = "SKU codes available for the A6000 series"
  value       = [for sku in data.gpcn_gpu_inventory.a6000.inventory : sku.sku_code]
}
