# The import ID is the ID of the VPC and the ID of the VPN connection, joined by
# a slash. The key is not imported, so the first apply sends it again.
terraform import gpcn_vpc_vpn_connection.example <vpc_id>/<connection_id>
