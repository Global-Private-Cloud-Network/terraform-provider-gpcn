---
page_title: "Migrating from gpcn_network to VPCs"
subcategory: ""
description: |-
  Move GPCN™ configurations off the deprecated gpcn_network resource: rebuild VMs on VPC subnets and move adopted custom networks to gpcn_l2_segment.
---

# Migrating from `gpcn_network` to VPCs

This guide walks you through moving a configuration off `gpcn_network`, the GPCN™ (Global Private Cloud Network™) resource for legacy networks. You'll move VMs onto VPC subnets, and move custom networks that GPCN™ adopted into L2 segments over to `gpcn_l2_segment`.

When you're done, nothing in your configuration depends on `gpcn_network`, and your plans are empty.

GPCN™ networking is now VPC-based. Since version 1.4, the provider can't create a `gpcn_network`. Existing ones still read, rename and destroy, so nothing breaks until you're ready to move.

## Prerequisites

- Version 1.4 of the provider or later
- A backup of your state file, or a remote backend with state versioning
- For VMs: a `gpcn_vpc` and a `gpcn_vpc_subnet` in the VM's data center. The [Getting started](getting-started) guide shows both.

## Which case is yours

| Your configuration has | Follow |
|------------------------|--------|
| **A `gpcn_virtualmachine` with `network_ids`** | [Move a VM onto a VPC subnet](#move-a-vm-onto-a-vpc-subnet) |
| **A custom `gpcn_network` that now answers 404** | [Move an adopted custom network](#move-an-adopted-custom-network) |
| **A `gpcn_network` that still reads** | Nothing yet. Keep it until no VM uses it, then destroy it. |

## Move a VM onto a VPC subnet

A VM on a legacy network can't move onto a subnet. GPCN™ refuses that change, so the move means a new VM.

Under 1.4 or later, the first plan for such a block fails validation: `subnet_id` is required and `network_ids` is gone. If you rewrite the block first, the plan proposes to destroy the VM and create a new one. Take these steps before that plan.

First, remove the VM from state. It keeps running, outside Terraform:

```shell
terraform state rm gpcn_virtualmachine.<name>
```

Next, delete the block or rewrite it. A rewritten block needs `subnet_id` from a `gpcn_vpc_subnet`, and applying it creates a new VM on that subnet.

To bring data across, attach a `gpcn_volume` to the new VM. If a `gpcn_volume_attachment` pointed at the old VM, check where its block points now:

- **The VM block was deleted.** Remove the attachment from state too, with `terraform state rm`.
- **The VM block was rewritten.** The attachment is replaced along with the VM, with no extra step.

Volumes, GPU instances, SSH keys and resource groups upgrade in place.

## Move an adopted custom network

GPCN™ adopts custom networks into L2 segments. After the adoption, the old network answers 404, so `gpcn_network` can't manage it anymore. The segment is a new object with a new ID, and Terraform can't make the move on its own. Take these steps in order.

### Find the segment ID

Use the first route that works:

- The VM's network interfaces. A VM that was on the network now reports `l2SegmentId` on that interface.
- The adoption event in the audit log. Its target is the segment, and its `metadata.change.networkId` is the old network's ID. Reading it needs the `audit-log:manage` permission.

### Rewrite the block

Change the block to `gpcn_l2_segment`. Keep `name`, `datacenter_id` and `description`, and delete the legacy attributes: `network_type`, `cidr_block`, `gateway_ip`, `dns_servers`, `dhcp_start_address` and `dhcp_end_address`.

If the segment's name differs from the network's, use the segment's. When your organization already had a segment with that name in the data center, the adoption added `-2`, `-3` and so on. If you keep the old name, the first apply plans a rename you didn't ask for.

### Move the state

Swap the state entry. This doesn't touch the network itself:

```shell
terraform state rm gpcn_network.<name>
terraform import gpcn_l2_segment.<name> <segment-id>
```

Then run `terraform apply`. The plan is empty when the imported segment matches the block.

~> **Never destroy the old network entry.** The segment carries live traffic, and a destroy tears it down.

### Update references

Point every `gpcn_network.<name>.id` in outputs and modules at the segment. In a `gpcn_virtualmachine` block, move the ID from `network_ids` to `l2_segment_ids`, which is where a segment attaches.

## Next Steps

- [`gpcn_vpc`](../resources/vpc) and [`gpcn_vpc_subnet`](../resources/vpc_subnet) — build the network your VMs move onto
- [`gpcn_l2_segment`](../resources/l2_segment) — the resource that replaces custom networks
- [VPCs on the GPCN™ Hub](https://hub.gpcn.com/docs/resources/networking/vpcs/overview) — how VPCs, subnets and security groups fit together
