terraform {
  required_providers {
    gpcn = {
      source = "gpcn.com/dev/gpcn"
    }
  }
}

# Instantiate the provider to verify required parameters are successfully set
provider "gpcn" {}
