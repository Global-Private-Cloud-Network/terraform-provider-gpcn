
# Example: Managing GPCN SSH Keys
#
# SSH keys can be managed in two modes:
#   - Upload mode: provide your own public key via public_key
#   - Generate mode: let the platform generate a key pair; specify algorithm (and optionally passphrase)
#
# The private_key output is only populated in generate mode and is stored in Terraform state.

terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~>0.4.2"
    }
  }
}

provider "gpcn" {
  host = "https://api.gpcn.com"
}

# Upload mode: provide an existing public key
resource "gpcn_ssh_key" "uploaded" {
  name = "terraform-demo-key-uploaded"
  # Not a real secret
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}

# Generate mode: platform generates an ed25519 key pair
resource "gpcn_ssh_key" "generated" {
  name      = "terraform-demo-key-generated"
  algorithm = "ed25519"
}

# Generate mode with passphrase
resource "gpcn_ssh_key" "generated_with_passphrase" {
  name       = "terraform-demo-key-generated-pass"
  algorithm  = "rsa-4096"
  passphrase = "my-secret-passphrase"
}

# Access the generated private key (sensitive)
output "generated_private_key" {
  value     = gpcn_ssh_key.generated.private_key
  sensitive = true
}
