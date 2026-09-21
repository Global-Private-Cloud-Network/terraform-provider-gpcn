# CLAUDE.md

## Project Overview

Terraform provider for GPCN (cloud infrastructure platform) built with Terraform Plugin Framework (v1.19.0).

Resources: `gpcn_gpu`, `gpcn_network`, `gpcn_resource_group`, `gpcn_ssh_key`, `gpcn_virtualmachine`, `gpcn_volume`, `gpcn_volume_attachment`.

Data sources: `gpcn_datacenters`, `gpcn_gpu_inventory`, `gpcn_virtualmachine_images`, `gpcn_virtualmachine_sizes`.

Registration lives in `Resources()` / `DataSources()` in `internal/provider/provider.go`.

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
make test     # Run unit tests (no API credentials)
make coverage # Unit tests with a coverage profile plus coverage.html
make testacc  # Run acceptance tests (creates real resources, requires credentials)
make testaccnamed TEST='TestNetworksResource$'  # Run one test
make testacc LOGLEVEL=debug  # Control log level
```

`make test` needs a `terraform` binary, taken from `PATH` or downloaded by
terraform-plugin-testing when `PATH` has none. The `resource.UnitTest` cases run
real terraform: the validator cases stop at validation, and the plan-behaviour
tests (`internal/provider/*_resource_plan_test.go`) drive terraform against an
`httptest` mock of the GPCN API. No API credentials are needed.

## Architecture

### Resource Package Pattern

Packages under `internal/{resource}/` usually contain:

- `resource_model.go`: Terraform state model structs
- `crud_actions.go`: HTTP request/response logic and API calls
- `plan_modifiers.go`: Custom plan modifiers (optional)
- `validators.go`: Custom validators (optional)
- `logging.go`: Structured logging constants
- `errors.go`: Error message constants
- `constants.go`: API endpoints and other constants

That is the usual shape, not a rule. Packages add topic files: `networks` has
`network_interfaces.go` and `default_route.go`; `virtualmachines` has
`lifecycle.go`, `sizes.go`, `update_helpers.go`; `volumes` has `sizes.go` and
`virtualmachines.go`; `gpu` has `inventory.go`.

Exceptions:

- `internal/volumeattachments` has no `constants.go` — it composes `volumes` and reuses its endpoints.
- `internal/datacenters` holds only constants and errors; its HTTP is inline in `internal/provider/datacenter_data_source.go`. Copy the newer data-source packages (`internal/virtualmachinesizes`, `internal/virtualmachineimages`) instead.

Shared helpers live in `internal/helpers` and `internal/testutil`.

Resource schema definitions live in `internal/provider/{resource}_resource.go`.

### Key Design Patterns

1. **Separation of Concerns**: `internal/provider/` handles Terraform framework integration; `internal/{resource}/` handles API communication
2. **Async Operations**: Only endpoints that return a job are polled — networks, volumes, virtual machines, GPUs, and attachments. Resource groups and SSH keys are synchronous. `internal/client/polling.go` long-polls until completion. Two envelope shapes exist: `client.JobStatusSingularResponse` (`data.jobId`) and `client.JobStatusMultiResponse` (`data.jobs[]`, read via `client.GetJobID`). The jobs endpoint constant lives in `internal/client/constants.go`
3. **Read refresh**: `gpcn_gpu`, `gpcn_network` and `gpcn_virtualmachine` have a `Refresh<X>ModelFromResponse` that Read calls after the mapper. Each one refreshes `name` only, because Terraform reconciles a rename in place. Every other configured attribute keeps the configured value, because reconciling an out-of-band change to it can destroy the resource. On `gpcn_virtualmachine` that covers `size_id`: a refreshed value plans a downgrade, and the API refuses one. On `gpcn_volume` it covers `name` and `size_gb`, so neither refreshes and the resource has no such function. A drifted volume name requires replacement, and a drifted grow plans a shrink that requires one. `gpcn_ssh_key` and `gpcn_resource_group` refresh `name` inside their shared mapper, which Create and Update call too. `gpcn_volume_attachment` refreshes nothing; its Read compares the attached VM id. Create and Update keep the planned values
4. **Error/Logging Constants**: Centralized in each resource's `errors.go` and `logging.go`
5. **API Versioning**: All endpoints use versioned paths (e.g., `/v1/resource/virtual-machines/`), defined in each resource's `constants.go`
6. **Internal import direction**: `client` and `helpers` are leaves; `networks` builds on them, `virtualmachines` on `networks`, `volumeattachments` on `volumes` and `client` only. Keep it acyclic
7. **Plan-test mocks serve the preflight**: `Configure` calls `GET /v1/auth/check` before any resource work, so every `httptest` mock behind a `resource.UnitTest` must answer it with `testutil.HandleAuthCheck`, or every step fails with `Cannot reach the GPCN API`
8. **`gpcn_network` is deprecated**: `ModifyPlan` refuses a create (prior state null) with `ErrDetailNetworkCreateRetired`; existing networks still read, update and destroy. GPU series are validated against the live inventory (`CheckInventory`), never a fixed list. `volume_type` accepts `SSD`, `NVMe` or a storage component code; the built-in codes `vol-add-ssd` and `vol-add-nvme` are refused at plan in favour of the alias; import writes the alias for the built-in classes

### Virtual Machine Specifics

- `size_id` is required: a SKU ID from the `gpcn_virtualmachine_sizes` data source (categories are `general-purpose` and `memory-optimized`)
- Whether a `size_id` change is an in-place update or a replacement is decided in `ModifyPlan`, which asks the API for the VM's legal upgrade targets (`GET /v1/resource/data-centers/{id}/virtual-machine-sizes?vmId=`). A lookup failure fails the plan rather than proposing a destroy
- `image_id` comes from the `gpcn_virtualmachine_images` data source; changing it requires replacement
- `initial_auth` is create-only: later changes update Terraform state with no API call
- `allocate_public_ip` controls whether `public_ip` is populated; `network_interfaces` is computed, one entry per attached network

### GPU Specifics

- Specify GPU series by `series_name` (human-readable) or `series_code`; exactly one required
- `sku_code` is optional and pins an exact SKU within the series; discover SKUs with the `gpcn_gpu_inventory` data source
- `gpu_count` must be 1, 2, 4, or 8
- `image_name` specifies the OS image; must be `"ubuntu-22.04"` or `"ubuntu-24.04"` (required, changing requires replacement)
- `initial_auth.ssh_key_id` is required and create-only
- Inventory is checked before creation via `CheckInventory()`

## Testing

The only unit/acceptance split is `TF_ACC`: `resource.Test` cases skip without it,
`resource.UnitTest` cases always run. An acceptance test is any function that
calls `resource.Test`; there is no `TestAcc*` prefix in this repo, and the name
carries no marker either — `TestNetworksResource` and
`TestVirtualMachinesSizeUpgrade` are both acceptance cases, while
`TestNetworksResourceInvalidType` is a `resource.UnitTest` one. Identify an
acceptance test by that call, and pass its exact function name to
`make testaccnamed TEST=...`, anchored with a trailing `$`. The target's
`-run` regexp is unanchored, so a bare `TestNetworksResource` also runs the
five longer names that start with it.

- **Unit tests**: `testutil.SetupMockServerWithGpcnClient` (`internal/testutil/mock_http.go`) serves mocked HTTP. It bypasses `authTransport`, so not-found and `HTTPError` paths cannot be tested through it — use `testutil.SetupMockServerWithRealTransport`, or `client.NewGpcnClient` against an `httptest` server, for those. Run with `make test`.
- **Acceptance tests**: Create real resources, and there are no sweepers, so a failed run leaves them behind. Run with `make testacc`. Run individual tests to iterate faster. The network and virtual machine cases read `GPCN_TEST_NETWORK_ID` (an existing network the key can see) and skip when it is unset, because the provider refuses to create a `gpcn_network`.

## Documentation

Generated via `make generate` using terraform-plugin-docs. Examples in `examples/resources/gpcn_{resource}/`.

## Code Quality

### Linting

The project uses golangci-lint (`.golangci.yml`). Key enabled linters:

- `gosec`: Security scanner - use `//nolint:gosec` with explanation for false positives
- `bodyclose`: Ensures HTTP response bodies are closed
- `contextcheck`: Validates context usage
- `errorlint`: Proper error wrapping patterns
- `noctx`: Ensures HTTP requests use context

Run `make lint` before committing.

### Code Style Requirements

1. **Handle all return values**: Never use blank identifiers (`_`) to ignore errors from `types.*ValueFrom()`. Always check `diag.Diagnostics` and handle errors (typically by setting a null value as fallback).

2. **Unexport internal fields**: Struct fields used only within a package should be unexported (lowercase), e.g., `apiKey` not `ApiKey`.

3. **gosec annotations**: When suppressing gosec warnings, always include an explanation:
   ```go
   //nolint:gosec // G704: URL is constructed from validated config, not user input
   ```

### GPCN Client

The `internal/client/` package provides a configurable HTTP client:

- **Correlation IDs**: Nothing adds one automatically. Call `client.WithCorrelationID(ctx)` at the start of a CRUD method; only then does the ID appear in logs and in the request header.

- **Configurable timeouts**: Users can customize via provider config:
  - `request_timeout`: Individual HTTP request timeout (default: 60s)
  - `polling_timeout`: Max wait for async operations (default: 10m)
  - `max_retries`: Retry count for transient failures (default: 3)

- **Retry with backoff**: Use `client.DoWithRetry(req)` for requests that should retry on transient failures.

### CI

- `test.yml` (PRs and `main`): lint, unit tests with terraform installed, build, and a docs-drift check that regenerates docs and fails on any difference.
- `security.yml`: govulncheck and CodeQL, on PRs, `main`, and weekly.
- `release.yml`: goreleaser on `v*` tags, with GPG-signed checksums.
- Dependabot: gomod at `/` and GitHub Actions, both weekly.
- Acceptance tests never run in CI.

## Commits

Read `.claude/rules/commit-conventions.md` before you commit, push, or open a pull request, including a draft pull request.

## Releasing

To prepare a new release:

1. Update `CHANGELOG.md` with the new version and release notes, and replace `(Unreleased)` with the release date
2. Update the provider version in all example `.tf` files under `examples/`:
   - `examples/resources/gpcn_*/resource.tf`
   - `examples/data-sources/gpcn_*/data-source.tf`
   - `examples/provider-install-verification/main.tf`
3. Run `make` to regenerate documentation (this copies examples into `docs/`)
4. Commit all changes
5. Create the tag (`git tag vX.Y.Z`) and ask the user to push it — pushing needs explicit authorization, see `.claude/rules/commit-conventions.md`

## MCP Servers

Always use Context7 when I need library/API documentation, code generation, setup or configuration steps without me having to explicitly ask.
