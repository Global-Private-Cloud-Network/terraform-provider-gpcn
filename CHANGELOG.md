## 1.0.0 (June 15, 2026)

BREAKING CHANGES:

- **Virtual Machines**: The `auth` block has been renamed to `initial_auth`. Existing configurations must be updated.
- **GPU**: The `auth` block has been renamed to `initial_auth`. Existing configurations must be updated.
- **Virtual Machines**: The `image` attribute has been renamed to `image_id`. Use the new `gpcn_virtualmachine_images` data source to look up image IDs.
- **Virtual Machines**: The `volume_ids` attribute has been removed. Volumes must now be attached using the new `gpcn_volume_attachment` resource.
- **Virtual Machines**: The `size` block has been replaced by a `size_id` string attribute. Use the new `gpcn_virtualmachine_sizes` data source to look up size IDs.
- **Virtual Machines**: The VM size category value `"general"` has been renamed to `"general-purpose"` and `"memory"` has been renamed to `"memory-optimized"`.

FEATURES:

- **Data Source: `gpcn_virtualmachine_images`**: New data source to list available OS images for a given datacenter, with optional filtering by image type or name substring.
- **Data Source: `gpcn_virtualmachine_sizes`**: New data source to list available VM sizes for a given datacenter, with optional filtering by category, minimum CPU, minimum memory, and minimum base storage size.
- **Resource: `gpcn_volume_attachment`**: New resource for attaching a volume to a virtual machine. Replaces the `volume_ids` attribute on `gpcn_virtualmachine`.
- **Datacenters**: `gpcn_datacenters` data source now exposes `gpu_enabled` and `custom_images` boolean attributes for filtering datacenters by capability.

ENHANCEMENTS:

- **Virtual Machines**: Size upgrades within the same category are again performed in-place without requiring resource replacement.

## 0.5.4 (May 12, 2026)

ENHANCEMENTS:

- **GPU**: Updated GPU structure and inventory to match API changes; GPU availability now determined via `availableSkus` using `skuId`
- **Volumes**: Updated volume size selection to use `skuId`-based API model
- **Virtual Machines**: Updated VM size selection to use `skuId`-based API model
- **Virtual Machine images**: Image validation now emits a warning instead of an error when an unrecognized image is specified, preventing provider breakage when new images are added before the provider is updated
- Upgraded Go dependencies

BREAKING CHANGES:

- **Virtual Machines**: Updating a virtual machine's size is no longer done in-place and requires a destroy and re-create. This will be reverted when the GPCN API SKU updates have been finalized
- **GPU**: Updated valid GPU series name and codes list

## 0.5.3 (April 16, 2026)

BUG FIXES:

- **Virtual Machine images**: Updated image lookup to handle the new nested category structure returned by the API. Image IDs are now strings (UUIDs) instead of integers, matching the current API format.

## 0.5.2 (March 30, 2026)

FEATURES:

- **Resource Group Resource**: New `gpcn_resource_group` resource for creating and managing resource groups. Virtual machines can now be attached to a resource group via the `resource_group_id` attribute on `gpcn_virtualmachine`.

## 0.5.1 (March 29, 2026)

BUG FIXES:

- **Datacenter `country_id`**: Fixed `country_id` field in the `gpcn_datacenters` data source to match updated API response format

ENHANCEMENTS:

- Upgraded Terraform plugin dependencies: terraform-plugin-framework 1.19.0, terraform-plugin-go 0.31.0, terraform-plugin-testing 1.15.0

## 0.5.0 (March 18, 2026)

BREAKING CHANGES:

- **Network `dns_servers`**: The `dns_servers` attribute on `gpcn_network` is now a list of strings instead of a comma-delimited string. Existing configurations must be updated (e.g., `dns_servers = "8.8.8.8,1.1.1.1"` → `dns_servers = ["8.8.8.8", "1.1.1.1"]`)
- **Virtual Machine `auth` block**: The `auth` block is now mandatory on `gpcn_virtualmachine`. The previous `display_secrets`/`secrets` logic has been removed; credentials are now user-provided via the `auth` block
- **GPU `auth` block**: `gpcn_gpu` now requires an `auth` block containing an `ssh_key_id` to associate an SSH key with GPU instances
- **Virtual machine `wait_for_startup`**: As the API has evolved, this has become less necessary and is now the default behavior

FEATURES:

- **SSH Key Resource**: New `gpcn_ssh_key` resource for managing SSH public keys uploaded to GPCN

ENHANCEMENTS:

- Acceptance tests can now run in parallel for faster CI feedback

BUG FIXES:

- Fixed ImportState case for GPUs where the image name wasn't properly being set

## 0.4.2 (March 11, 2026)

FEATURES:

- **Network Hotplug Support**: Virtual machines now check `network_hotplug` capability to determine if they need to be in Shutoff status before making network or volume changes. VMs with hotplug enabled can be modified while running.

ENHANCEMENTS:

- Added 30-second delay before polling for virtual machine status after creation, improving reliability for VMs that take longer to initialize
- Upgraded Go dependencies from 1.26.0 to 1.26.1 to fix built-in vulnerabilities

## 0.4.1 (March 5, 2026)

FEATURES:

- **GPCN Client is now more configurable**: New provider configuration options for customizing HTTP behavior:
  - `request_timeout`: Individual HTTP request timeout (default: 60s)
  - `polling_timeout`: Maximum wait time for async operations (default: 10m)
  - `max_retries`: Retry count for transient failures (default: 3)
- **Correlation IDs**: All API requests now include correlation IDs for improved tracing and debugging

ENHANCEMENTS:

- Added GitHub Actions workflows for automated testing, security scanning, and dependency management
- Added golangci-lint configuration for consistent code quality
- Added Dependabot configuration for automated dependency updates
- Upgraded Go dependencies from 1.25.0 to 1.26.0

BUG FIXES:

- Fixed custom network creation and updates to match latest API changes
- Fixed VM creation to reflect latest API changes
- Fixed gosec security issues with explanatory annotations
- Resolved dependency vulnerabilities

## 0.4.0 (February 23, 2026)

BREAKING CHANGES:

- **GPU Resource**: Added required `image_name` attribute to `gpcn_gpu` resource. Existing configurations must be updated to include this attribute. Valid values are `"ubuntu-22.04"` or `"ubuntu-24.04"`

ENHANCEMENTS:

- Updated GPU inventory response handling to use `availableSkus` array instead of `available` count
- Marked virtual machine `secrets` attribute as sensitive in the schema

## 0.3.2 (January 22, 2026)

FIXES:

- Fixed case where state could be tainted due to public_ip defaulting to empty string and then being replaced

## 0.3.1 (January 22, 2026)

FEATURES:

- **Virtual Machine Secrets**: Added `display_secrets` optional attribute and `secrets` computed attribute to virtual machines
  - When `display_secrets` is true, fetches username, password, and SSH private key from the API
  - Secrets are stored in Terraform state (warning included in schema description)
- **Virtual Machine Public IP**: Added `public_ip` computed attribute to virtual machines, populated when `allocate_public_ip` is true

ENHANCEMENTS:

- Extracted custom plan modifiers into dedicated `plan_modifiers` file for virtual machines

TESTING:

- Significantly reduced complexity of unit tests across all resource packages (networks, volumes, virtual machines, GPUs)

## 0.3.0 (January 10, 2026)

FEATURES:

- **GPU Resource**: Added new `gpcn_gpu` resource for managing dedicated GPU instances
  - Support for multiple GPU series: H100 Series, A100 Series, RTX A6000 Series, RTX PRO 6000 Blackwell Series
  - Flexible series specification via human-readable `series_name` or machine-readable `series_code`
  - Automatic series name to code lookup using static mapping
  - GPU count options: 1, 2, or 4 GPUs per instance
  - Inventory validation to ensure availability before creation
  - Datacenter-specific deployment
  - Full lifecycle support including terraform import

ENHANCEMENTS:

- Refactored CRUD response handling across all resources to use anonymous inline structs for improved code readability
- Enhanced structured logging throughout all resource operations
- Improved error messaging consistency
- Import logic now correctly imports missing attributes for all resources
  - Known issue: Importing a gpcn_virtualmachine from scratch does not import attached volumes

TESTING:

- Added comprehensive unit tests for GPU resources using mock HTTP servers
- Added acceptance tests for GPU resource lifecycle operations
- Improved datacenter specification in tests to avoid region-specific issues

BUG FIXES:

- Removed extraneous error messages that weren't used
- Fixed linting errors across the codebase
- The name parameter in datacenters now correctly filters by that name, instead of doing nothing

## 0.2.0 (January 05, 2026)

BREAKING CHANGES:

- Virtual machine sizing logic has been restructured to use category and tier codes, aligning with GPCN API changes

FEATURES:

- **Virtual Machines**: Added support for specifying category and tier codes directly, enabling broader VM sizing configurations beyond General Purpose
- **Import Improvements**: Enhanced import functionality for virtual machines to include network interfaces and comprehensive state information

ENHANCEMENTS:

- Simplified virtual machine sizing configuration by removing separate `additionalImages` and `additionalSizes` response objects
- Updated virtual machine response models to reflect GPCN API changes for size configurations
- Improved logging throughout virtual machine operations with consistent ID references
- Enhanced network interface handling for virtual machine resources

TESTING:

- Added unit test coverage for networks, volumes, and virtual machines
- Refactored acceptance tests to improve clarity

BUG FIXES:

- Importing a virtualmachine into Terraform state should now correctly fetch related information

DOCUMENTATION:

- Updated virtual machine resource documentation to reflect new category and tier code attributes

## 0.1.2 (December 23, 2025)

ENHANCEMENTS:

- Updated all API endpoints to use versioned paths
- Added documentation to provider configuration attributes
- Improved consistency in error messages and ID field references across all resources

DOCUMENTATION:

- Enhanced provider configuration documentation with usage guidance
- Updated examples for networks, virtual machines, and volumes with improved clarity
- Added better explanations for how to use example files locally during development

## 0.1.1 (December 04, 2025)

ENHANCEMENTS:

- Added LICENSE file to repository
- Updated documentation with improved examples and clarity
- Updated provider source address to `Global-Private-Cloud-Network/gpcn` in examples

## 0.1.0 (Initial Release)

**Initial public release of the GPCN Terraform Provider**

This is the first release of the official Terraform provider for GPCN, enabling infrastructure-as-code management of GPCN cloud resources.

FEATURES:

**Resources:**

- **gpcn_network** - Manage virtual networks with support for standard (fully managed) and custom network types
  - CIDR block configuration with custom IP ranges
  - DHCP allocation pool management
  - DNS server configuration
  - Automatic SNAT, gateway, and DHCP for standard networks
  - Full lifecycle support including terraform import

- **gpcn_volume** - Manage block storage volumes
  - SSD and NVMe storage types
  - Dynamic volume sizing with growth support (size increases without replacement)
  - Multi-VM attachment capability (volumes can be attached to up to 5 VMs)
  - Datacenter-specific deployment
  - Full lifecycle support including terraform import

- **gpcn_virtualmachine** - Manage virtual machine instances
  - Flexible compute sizing with CPU/RAM/Disk configuration
  - Multiple OS image support per datacenter
  - Network interface management (up to 5 networks per VM)
  - Volume attachment support (up to 5 volumes per VM)
  - Public IP allocation control
  - Power state management with automatic start on creation
  - Smart lifecycle operations (automatic VM stop/start during updates when needed)
  - Size upgrades without replacement (downgrades require replacement)
  - Full lifecycle support including terraform import

**Data Sources:**

- **gpcn_datacenters** - Query available datacenters with multi-level filtering
  - Filter by country, region, or datacenter name
  - Hierarchical geographic organization
  - Complete location metadata (country, region, datacenter details)

**Provider Features:**

- API key authentication with secure credential handling
- Environment variable configuration support (GPCN_API_KEY, GPCN_HOST)
- Built with Terraform Plugin Framework v1.16.1
- Asynchronous operation support with long polling (10-minute timeout)
- Comprehensive error handling with actionable messages
- Structured logging for debugging
- Masked logging for sensitive values

**Developer Experience:**

- Full acceptance test suite with automatic resource cleanup
- Local development setup via .terraformrc dev overrides
- Comprehensive documentation in docs/ directory
- Example configurations for all resources
- Makefile with common development tasks (build, test, lint, format)

TECHNICAL DETAILS:

- Built with Go 1.24.0
- Terraform Plugin Framework v1.16.1
- Smart state management with conditional updates
- Resource import support for all resources
- Long polling for asynchronous operations (3-second interval, 600-second timeout)

KNOWN LIMITATIONS:

- Maximum 5 networks per VM
- Maximum 5 volumes per VM
- Volume can only be attached to one VM at a time
- Network type changes require resource replacement
- VM image changes require resource replacement
- VM size downgrades require resource replacement
- Volume size can grow but shrinking requires resource replacement
