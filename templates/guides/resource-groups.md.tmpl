---
page_title: "Organizing with resource groups"
subcategory: ""
description: |-
  Choose a resource group for each GPCN™ resource, move resources between groups, and manage the groups themselves.
---

# Organizing with resource groups

This guide shows you how the GPCN™ (Global Private Cloud Network™) provider places resources in resource groups. You'll pick a group for each resource, look up your organization's Default group, move resources between groups, and delete a group safely.

By the end you'll know which group each resource lands in, and what Terraform does when you change it.

What resource groups are, and how roles scope to them, is covered on the GPCN™ Hub in [Resource groups](https://hub.gpcn.com/docs/identity-and-access/people/resource-groups) and [Roles and permissions](https://hub.gpcn.com/docs/identity-and-access/roles-and-permissions). This guide covers the Terraform side.

## Prerequisites

- Version 1.5 of the provider or later
- An API key whose role is scoped to every group you'll create resources in or move them to. An organization-wide role acts on resources in the Default group only.

## Which resources have a group

Five resources take a `resource_group_id`:

| Resource | Notes |
|----------|-------|
| **`gpcn_virtualmachine`** | |
| **`gpcn_gpu`** | |
| **`gpcn_volume`** | |
| **`gpcn_vpc`** | Its subnets, security groups and elastic IPs follow the VPC's group |
| **`gpcn_l2_segment`** | |

The attribute is optional. If you leave it out, the resource joins your organization's Default group, and `resource_group_id` reports that group's ID after the apply.

## Put a resource in a group

Create the group with `gpcn_resource_group`, then name it on each resource:

```terraform
resource "gpcn_resource_group" "web" {
  name        = "web"
  description = "Web tier"
}

resource "gpcn_vpc" "web" {
  name              = "web"
  datacenter_id     = data.gpcn_datacenters.chicago.datacenters[0].id
  cidr              = "10.30.0.0/16"
  resource_group_id = gpcn_resource_group.web.id
}
```

To use a group someone else created, look it up by name with the `gpcn_resource_groups` data source:

```terraform
data "gpcn_resource_groups" "web" {
  name = "web"
}

# data.gpcn_resource_groups.web.resource_groups[0].id
```

The data source fails if no group by that name is visible to your API key.

## Find the Default group

The same data source reports your organization's Default group as `default_resource_group_id`:

```terraform
data "gpcn_resource_groups" "all" {}

output "default_group" {
  value = data.gpcn_resource_groups.all.default_resource_group_id
}
```

The Default group exists from the start, and you can't delete it. In the data source's `resource_groups` list, it's the entry with `is_default = true`.

## Move a resource to another group

Change `resource_group_id` and apply. The resource moves in place: Terraform doesn't recreate it, and its ID stays the same.

Moving a VPC moves its subnets, security groups and elastic IPs with it.

To move a resource back to the Default group, set `resource_group_id` to `default_resource_group_id` from the data source. Removing the attribute from the block doesn't move anything. The resource stays in the group it's in.

If someone moves a resource in the portal, the next plan shows the move. If your configuration doesn't set `resource_group_id`, Terraform accepts the new group. If it does, Terraform plans to move the resource back.

## Replacing a resource

When a change replaces a resource, such as a new `image_id` on a VM, the new resource is created fresh. If your block doesn't set `resource_group_id`, the replacement joins the Default group, not the old resource's group. Set the attribute to keep it where it was.

## Delete a group

Destroying a `gpcn_resource_group` works only when the group is empty. Two cases are refused, and the error tells you what to do:

- **The Default group.** GPCN™ never deletes it. To stop managing it with Terraform, run `terraform state rm` on its address.
- **A group that still holds resources.** The error lists what's left, such as `(virtual machines: 2, volumes: 3)`. Move or destroy those resources first.

If one apply moves resources out of a group and destroys the group, Terraform deletes the group first, and the delete is refused. Apply the moves on their own, for example with `-target`, then apply again.

## Next Steps

- [`gpcn_resource_group`](../resources/resource_group) — the resource group resource
- [`gpcn_resource_groups`](../data-sources/resource_groups) — look up groups and the Default group
- [Resource groups on the GPCN™ Hub](https://hub.gpcn.com/docs/identity-and-access/people/resource-groups) — members, roles and the group model
- [Upgrading to 1.5](upgrading-to-1-5) — what changed for resource groups in 1.5
