# A security group is read through its VPC, so the import ID names both:
# "<vpc_id>/<nsg_id>".
terraform import gpcn_vpc_nsg.example "c13808d9-3b7d-42c5-a21d-f0961308a38a/4a6c1f38-92b1-4e77-8d50-6f3c2a1b7e94"
