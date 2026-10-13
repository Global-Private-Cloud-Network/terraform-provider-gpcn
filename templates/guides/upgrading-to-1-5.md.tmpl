---
page_title: "Upgrading to version 1.5"
subcategory: ""
description: |-
  What changes when you upgrade the GPCN™ provider from 1.4 to 1.5, and what to check before your first apply.
---

# Upgrading to version 1.5

This guide walks you through upgrading the GPCN™ (Global Private Cloud Network™) provider from 1.4 to 1.5. It covers when to upgrade, what changes in your plans, and the one case that needs a change to your configuration.

When you're done, your state will be on 1.5 and your plans will be empty again.

Version 1.5 adds resource groups to VMs, GPU instances, volumes, VPCs and L2 segments. Every one of those resources now belongs to exactly one resource group. For how groups work in Terraform, see [Organizing with resource groups](resource-groups).

## Before you upgrade

Upgrade to 1.5 now. Version 1.4 doesn't work with the current GPCN™ API for a `gpcn_virtualmachine` that sets no `resource_group_id`. GPCN™ reports the Default group, 1.4 plans to remove it, and the API refuses that update. A VM created under 1.4 without a group fails with `Provider produced inconsistent result after apply` and is marked tainted.

## Upgrade the provider

Change the version constraint in your configuration:

```terraform
terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~> 1.5"
    }
  }
}
```

Then upgrade and plan:

```shell
terraform init -upgrade
terraform plan
```

Your state upgrades in place. For a block that sets no `resource_group_id`, the first plan is empty. After it, the attribute reports the group GPCN™ holds the resource in.

## What changes

- **Removing `resource_group_id` doesn't move a VM anymore.** It stays in its group. To move it to the Default group, set `resource_group_id` to `default_resource_group_id` from the `gpcn_resource_groups` data source.
- **A replacement joins the Default group** when its block sets no `resource_group_id`. Set the attribute to keep a replaced VM in its old group.
- **`resource_group_id = ""` is refused at plan.** Leave the attribute out instead.
- **A VM moved in the portal stays where it was moved**, if its block sets no `resource_group_id`. Version 1.4 planned to undo the move.

## If your key can't create in the Default group

Some API keys have roles scoped to particular resource groups, with no role for the Default group. A key like that must set `resource_group_id` on every VM, GPU instance, volume, VPC and L2 segment it creates. Without one, GPCN™ refuses the create with HTTP 403.

## New in 1.5

- `resource_group_id` on `gpcn_gpu`, `gpcn_volume`, `gpcn_vpc` and `gpcn_l2_segment`. Changing it moves the resource in place.
- The `gpcn_resource_groups` data source, which lists groups, finds one by name and reports the Default group.
- `is_default` on `gpcn_resource_group`.
- Clearer errors when a group can't be destroyed. The Default group can't be deleted, and a group that still holds resources lists what's left.

The full list is in the [changelog](https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn/blob/main/CHANGELOG.md).

## Next Steps

- [Organizing with resource groups](resource-groups) — choose groups, move resources and delete groups
- [Migrating from `gpcn_network` to VPCs](migrating-from-gpcn-network) — if you're upgrading from 1.3 or earlier
- [Roles and permissions on the GPCN™ Hub](https://hub.gpcn.com/docs/identity-and-access/roles-and-permissions) — which roles act in which groups
