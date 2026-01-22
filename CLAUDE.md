# CLAUDE.md

## Project Overview

Terraform provider for GPCN (cloud infrastructure platform) built with Terraform Plugin Framework (v1.16.1). Manages networks, volumes, virtual machines, and GPUs, plus a datacenter data source.

## Environment Setup

Required environment variables:
- `GPCN_API_KEY`: API key for authentication
- `GPCN_HOST`: Base URL for the GPCN API

Local development:
1. `go mod tidy && go install .`
2. Create `~/.terraformrc` with dev overrides pointing to your local GOBIN
3. Provider address: `gpcn.com/dev/gpcn`

## Build and Test Commands

```bash
make          # Format, lint, install, and generate (default)
make build    # Build only
make install  # Install locally
make fmt      # Format code
make lint     # Lint
make generate # Generate documentation
make test     # Run unit tests (no API credentials needed)
make testacc  # Run acceptance tests (creates real resources, requires credentials)
make testaccnamed TEST=TestAccNetworkResource_basic  # Run specific acceptance test
make testacc LOGLEVEL=debug  # Control log level
```

## Architecture

### Resource Package Pattern

Each resource follows this structure in `internal/{resource}/`:
- `resource_model.go`: Terraform state model structs
- `crud_actions.go`: HTTP request/response logic and API calls
- `plan_modifiers.go`: Custom plan modifiers (optional)
- `validators.go`: Custom validators (optional)
- `logging.go`: Structured logging constants
- `errors.go`: Error message constants
- `constants.go`: API endpoints and other constants

Resource schema definitions live in `internal/provider/{resource}_resource.go`.

### Key Design Patterns

1. **Separation of Concerns**: `internal/provider/` handles Terraform framework integration; `internal/{resource}/` handles API communication
2. **Async Operations**: Create/update/delete return job IDs; `internal/client/polling.go` long-polls until completion
3. **Error/Logging Constants**: Centralized in each resource's `errors.go` and `logging.go`
4. **API Versioning**: All endpoints use versioned paths (e.g., `/v1/resource/virtual-machines/`), defined in each resource's `constants.go`

### Virtual Machine Specifics

- Size uses `category` (`general`, `memory`) + `tier` (e.g., `g-small-1`)
- Tier upgrades within the same category don't require replacement; downgrades or category changes do
- `display_secrets` attribute controls whether `secrets` map (username, password, ssh_key) is populated
- `allocate_public_ip` controls whether `public_ip` is populated

### GPU Specifics

- Specify GPU series by `series_name` (human-readable) or `series_code`; exactly one required
- GPU count must be 1, 2, or 4
- Inventory is checked before creation via `CheckInventory()`

## Testing

- **Unit tests**: Use `MockTransport` from `internal/testutil/mock_http.go` to intercept HTTP calls. Run with `make test`.
- **Acceptance tests**: Create real resources. Run with `make testacc`. Run individual tests to iterate faster.

## Documentation

Generated via `make generate` using terraform-plugin-docs. Examples in `examples/resources/gpcn_{resource}/`.
