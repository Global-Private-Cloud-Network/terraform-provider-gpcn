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
