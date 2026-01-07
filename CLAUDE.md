# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Terraform provider for GPCN (cloud infrastructure platform) built using the Terraform Plugin Framework (v1.16.1). The provider manages three main resource types: networks, volumes, and virtual machines, plus a datacenter data source.

## Environment Setup

Required environment variables for running the provider or tests:

- `GPCN_API_KEY`: API key for authentication
- `GPCN_HOST`: Base URL for the GPCN API

Local development requires:

1. Run `go mod tidy` and `go install .`
2. Create `~/.terraformrc` (or at `go env GOBIN`) with dev overrides pointing to your local GOBIN
3. The provider address is `gpcn.com/dev/gpcn`

## Build and Test Commands

```bash
# Format, lint, install, and generate (default target)
make

# Build only
make build

# Install locally
make install

# Format code
make fmt

# Lint
make lint

# Generate documentation
make generate

# Run unit tests (non-acceptance)
make test

# Run full acceptance tests (creates real resources, slow)
make testacc

# Run specific acceptance test
make testaccnamed TEST=TestAccNetworkResource_basic

# Adjust test timeout with flag
TF_ACC=1 TF_LOG=warn go test -run=TestAccNetworkResource_basic -v -timeout 60m ./...

# Control log level
make testacc LOGLEVEL=debug
```

## Recent Changes (v0.2.0)

**Breaking Changes:**

- Virtual machine sizing now uses `category` and `tier` attributes instead of previous sizing configuration
- Categories: `general` (g- prefix) and `memory` (m- prefix)
- Tiers: `g-micro-1`, `g-small-1`, `g-medium-1`, `g-large-1`, `g-xl-1`
- Import operations now populate network interfaces and volume attachments

**API Versioning:**

- All API endpoints use versioned paths (e.g., `/v1/resource/virtual-machines/`)
- Constants defined in each resource package's `constants.go`

**Virtual Machine Sizing:**

- Size configuration uses a category + tier pairing system
- Example Terraform configuration:

  ```hcl
  resource "gpcn_virtualmachine" "example" {
    name          = "my-vm"
    datacenter_id = "dc-123"
    image         = "Ubuntu 24.04 LTS"

    size = {
      category = "general"  # or "memory"
      tier     = "g-small-1"  # g-micro-1, g-small-1, g-medium-1, g-large-1, g-xl-1
    }
  }
  ```

- Valid images are defined in `internal/virtualmachines/constants.go`
- VM size upgrades within the same category don't require replacement
- Tier downgrades or category changes require resource replacement

## Architecture

### Provider Structure

**Entry Point**: `main.go` creates the provider server at address `gpcn.com/dev/gpcn`

**Provider Implementation**: `internal/provider/provider.go`

- Configures authentication via environment variables or Terraform config
- Registers resources and data sources
- Creates HTTP client with auth transport

### HTTP Client (`internal/client/`)

**Authentication**: Uses custom `authTransport` that:

- Uses API key for authentication
- Injects API key in Authorization header for all requests
- Prepends base URL to all request paths
- 60-second timeout for synchronous operations

**Long Polling**: `polling.go` implements `PerformLongPolling()` for async operations

- Polls job status endpoint every 3 seconds
- 10-minute max timeout (600 seconds)
- Returns when job completes or fails

### Resource Package Pattern

Each resource (networks, volumes, virtualmachines) follows this consistent structure in `internal/{resource}/`:

- `resource_model.go`: Terraform state model structs
- `crud_actions.go`: HTTP request/response logic and API calls (Create, Read, Update, Delete)
- `validators.go`: Custom validators for resource attributes
- `logging.go`: Structured logging constants and helpers
- `errors.go`: Error message constants
- `constants.go`: API endpoints and other constants

**Resource Implementations** in `internal/provider/`:

- `networks_resource.go`
- `volumes_resource.go`
- `virtualmachines_resource.go`
- `datacenter_data_source.go`

Each resource file defines:

- Schema with attributes, validators, plan modifiers
- Configure() method to receive HTTP client from provider
- CRUD methods that delegate to the resource package's crud_actions functions
- ImportState() for terraform import support

### Key Design Patterns

1. **Separation of Concerns**: Resource files in `provider/` handle Terraform framework integration; resource packages in `internal/` handle API communication
2. **Async Operations**: Create/update/delete operations return job IDs and use long polling to wait for completion
3. **Error Handling**: Centralized error constants in each resource's `errors.go`
4. **Logging**: Structured logging using terraform-plugin-log with consistent messages defined in `logging.go`

### Resource-Specific Logic

**Networks** (`internal/networks/`):

- `network_interfaces.go`: Complex logic for attaching/detaching network interfaces to VMs
- Supports CIDR blocks, allocation pools, DNS servers

**Virtual Machines** (`internal/virtualmachines/`):

- `lifecycle.go`: VM power state management (start/stop)
- `images.go`: Image selection helpers
- `sizes.go`: VM size/flavor helpers with category and tier support
- `volumes.go`: Volume attachment logic
- `constants.go`: Defines valid VM categories, tiers, and image names
  - Categories: `general`, `memory`
  - Tier lists: `GeneralTiers`, `MemoryTiers`, `AllTiers`
  - Complete list of valid image names (Ubuntu, Windows, Rocky, Debian, etc.)

**Volumes** (`internal/volumes/`):

- `sizes.go`: Volume size helpers
- `virtualmachines.go`: VM attachment logic

## Testing

### Unit Tests

Unit tests use mock HTTP servers to test resource logic without making real API calls:

- `internal/networks/networks_unit_test.go`: Network resource unit tests
- `internal/virtualmachines/virtualmachines_unit_test.go`: VM resource unit tests
- `internal/volumes/volumes_unit_test.go`: Volume resource unit tests
- `internal/testutil/mock_http.go`: Mock HTTP server utilities
  - `MockTransport`: Custom RoundTripper for intercepting HTTP calls
  - `SetupMockServer()`: Creates mock server with configurable handlers

Run unit tests with `make test` (they don't require API credentials).

### Acceptance Tests

Acceptance tests are in `*_test.go` files alongside resource implementations. They:

- Create real resources against the configured GPCN environment
- Automatically destroy resources after test completion
- Can take significant time to run (hence long timeouts)
- Require valid GPCN credentials via environment variables

Run individual tests to iterate faster during development.

## Documentation

Provider documentation is in `docs/` and generated via `make generate` using terraform-plugin-docs. Examples for each resource are in `examples/resources/gpcn_{resource}/`.
