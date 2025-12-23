terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.1.0"
    }
  }
}

# Instantiate the provider to verify required parameters are successfully set
provider "gpcn" {
  host = "https://api.gpcn.com"
}
