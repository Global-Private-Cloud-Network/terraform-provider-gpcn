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
