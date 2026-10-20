# The import ID is the ID of the VPC and the ID of the VPN connection, joined by
# a slash. The key is not imported, so the first apply sends it again.
terraform import gpcn_vpc_vpn_connection.example "c13808d9-3b7d-42c5-a21d-f0961308a38a/8f1a2b3c-4d5e-6f70-8192-a3b4c5d6e7f8"
