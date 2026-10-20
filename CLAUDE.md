# CLAUDE.md

## Project Overview

Terraform provider for GPCN (cloud infrastructure platform) built with Terraform Plugin Framework (v1.19.0), Go 1.26.9 and terraform-plugin-testing v1.16.0 (`go.mod`), and terraform-plugin-docs v0.25.0 (`tools/go.mod`).

Resources: `gpcn_gpu`, `gpcn_l2_segment`, `gpcn_network` (deprecated; create refused), `gpcn_resource_group`, `gpcn_ssh_key`, `gpcn_virtualmachine`, `gpcn_volume`, `gpcn_volume_attachment`, `gpcn_vpc`, `gpcn_vpc_nsg`, `gpcn_vpc_public_ip`, `gpcn_vpc_public_ip_attachment`, `gpcn_vpc_subnet`, `gpcn_vpc_vpn_connection`, `gpcn_vpc_vpn_gateway`.

Data sources: `gpcn_datacenters`, `gpcn_gpu_inventory`, `gpcn_resource_groups`, `gpcn_virtualmachine_images`, `gpcn_virtualmachine_sizes`.

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
2. **Async Operations**: Only endpoints that return a job are polled — networks, volumes, virtual machines, GPUs, volume attachments, and the whole VPC family (VPC create and delete, subnet create, rebind and delete, security-group create, rule replace and delete, public IP acquire, attach, detach and release, L2 segment create and delete, VPN gateway create and delete, VPN connection create, update and delete). Renames, description changes and resource-group moves on the other new resources are synchronous (a `gpcn_vpc_vpn_connection` rename is a polled update), as are the volume move (`PUT /v1/resource/volumes/{id}`), resource groups and SSH keys. `internal/client/polling.go` long-polls until completion. Two envelope shapes exist: `client.JobStatusSingularResponse` (`data.jobId`) and `client.JobStatusMultiResponse` (`data.jobs[]`, read via `client.GetJobID`). The jobs endpoint constant lives in `internal/client/constants.go`
3. **Read refresh**: `gpcn_gpu`, `gpcn_network`, `gpcn_virtualmachine`, `gpcn_vpc`, `gpcn_vpc_subnet`, `gpcn_vpc_nsg` and `gpcn_l2_segment` have a `Refresh<X>ModelFromResponse` that Read calls after the mapper. Each one refreshes `name`, because Terraform reconciles a rename in place. Apart from `resource_group_id` (below), every other configured attribute keeps the configured value, because reconciling an out-of-band change to it can destroy the resource. On `gpcn_virtualmachine` that covers `size_id`: a refreshed value plans a downgrade, and the API refuses one. On `gpcn_volume` it covers `name` and `size_gb`, so neither refreshes and the resource has no such function. A drifted volume name requires replacement, and a drifted grow plans a shrink that requires one. `gpcn_ssh_key` and `gpcn_resource_group` refresh `name` inside their shared mapper, which Create and Update call too. `gpcn_volume_attachment` refreshes nothing; its Read compares the attached VM id. Create and Update keep the planned values. The VPC-family resources with a name (`gpcn_vpc`, `gpcn_vpc_subnet`, `gpcn_vpc_nsg`, `gpcn_l2_segment`) refresh `name` and `description` (the subnet also `nsg_id`, the security group also its rule set), because each reconciles in place; live counters such as `attached_nic_count` carry no plan modifier and plan Unknown. Every resource with a `resource_group_id` (`gpcn_virtualmachine`, `gpcn_gpu`, `gpcn_volume`, `gpcn_vpc`, `gpcn_l2_segment`) writes the API's value on Read, because a move reconciles in place, and on Create and Update only when the plan left it unknown (a known planned value is kept, so a move that lands mid-apply shows as drift on the next plan instead of failing the apply); the attribute is Optional+Computed with `UseNonNullStateForUnknown` (state written by 1.4.0 holds null, and `UseStateForUnknown` plans that null), the provider sends it on create when set and on update only when changed, and it never sends null or `""`. `gpcn_vpc_vpn_connection` refreshes every configured attribute except `psk`, `psk_wo_version` and `vpc_id`, because each reconciles in place and the API never returns the key; `vpn_gateway_id` is refreshed from the VPC's gateway; `gpcn_vpc_vpn_gateway` configures only `vpc_id`.
4. **Error/Logging Constants**: Centralized in each resource's `errors.go` and `logging.go`
5. **API Versioning**: All endpoints use versioned paths (e.g., `/v1/resource/virtual-machines/`), defined in each resource's `constants.go`
6. **Internal import direction**: `client`, `helpers` and `datacenters` are leaves. `l2segments`, `vpcs`, `vpcsubnets`, `vpcnsgs`, `vpcpublicips`, `networks`, `volumes`, `gpu`, `sshkeys`, `resourcegroups`, `virtualmachineimages` and `virtualmachinesizes` import `client` (and `helpers` where they validate) only; `vpcvpngateways` imports `client` only; `vpcvpnconnections` imports `vpcvpngateways` and `client`; `volumeattachments` imports `volumes` and `client`; `virtualmachines` imports `networks`, `virtualmachineimages`, `vpcpublicips` and `client`, and `vpcpublicips` must never import `virtualmachines`. `testutil` imports `client` only; `provider` imports all of them and nothing imports `provider` except `main.go` and external `_test` packages (e.g. `l2segments/schema_guard_test.go`). Only `sshkeys` imports `helpers` among the resource packages; `provider` imports it for the IPv4 and CIDR validators and the whitespace validator. Keep it acyclic
7. **Plan-test mocks serve the preflight**: `Configure` calls `GET /v1/auth/check` before any resource work, so every `httptest` mock behind a `resource.UnitTest` must answer it with `testutil.HandleAuthCheck`, or every step fails with `Cannot reach the GPCN API`
8. **`gpcn_network` is deprecated**: `ModifyPlan` refuses a create (prior state null) with `ErrDetailNetworkCreateRetired`; existing networks still read and destroy until the API answers 404; GPCN™ has retired legacy networks, so Read then drops the network from state. GPU series are validated against the live inventory (`CheckInventory`), never a fixed list. `volume_type` accepts `SSD`, `NVMe` or a storage component code; the built-in codes `vol-add-ssd` and `vol-add-nvme` are refused at plan in favour of the alias; import writes the alias for the built-in classes

### Virtual Machine Specifics

- `size_id` is required: a SKU ID from the `gpcn_virtualmachine_sizes` data source (categories are `general-purpose` and `memory-optimized`)
- Whether a `size_id` change is an in-place update or a replacement is decided in `ModifyPlan`, which asks the API for the VM's legal upgrade targets (`GET /v1/resource/data-centers/{id}/virtual-machine-sizes?vmId=`). A lookup failure fails the plan rather than proposing a destroy
- `image_id` comes from the `gpcn_virtualmachine_images` data source; changing it requires replacement
- `initial_auth` is create-only: later changes update Terraform state with no API call
- Networking is VPC-based: `subnet_id` is required and replaces the machine when changed; `l2_segment_ids` (up to four) attaches layer-2 segments after the machine exists, with a stop and start when the image has no network hotplug; `allocate_public_ip` (optional, default false) acquires an elastic address on the VPC, `public_ip_id` attaches one the operator holds, and the two conflict. An import never infers `allocate_public_ip`: it records the address as `public_ip_id`. `network_interfaces` is computed, one entry per interface, and reports the interface's world (`vpc` or `l2`)
- Update runs every step after a stop through one runner: a failed step starts the machine again, and a failed start reports a `Virtual machine left stopped` detail that says what to do next. A retry never repeats work the live machine already carries (segment adds are computed against the live interface list; a resize the machine already has is treated as done; the stop decision compares the plan against the live machine)

### VPN Specifics

- `gpcn_vpc_vpn_gateway` is a singleton per VPC. Its API route carries the VPC id and no gateway id, so import takes the VPC id. A gateway that reads `failed` plans replacement (`ModifyPlan` marks `state` unknown and requires replace on it), because the API has no retry
- `gpcn_vpc_vpn_connection` takes the key as `psk` (sensitive, stored) or `psk_wo` with `psk_wo_version` (write-only, Terraform 1.11+); exactly one. The API never returns the key, so Read keeps the state value. The key is never logged: `vpcvpnconnections/logging.go` carries no request body. Import is `<vpc_id>/<connection_id>` and leaves `psk` and `psk_wo_version` null, so the first apply re-sends the key
- `peer_address` and `peer_cidrs` are checked at plan by `helpers.IPv4AddressValidator` and `helpers.IPv4CIDRValidator`: no leading zeros, and a CIDR with host bits is refused because the API rewrites it and the plan would never settle
- A connection adds ingress rules to the security groups of the VPC. `gpcn_vpc_nsg` hides rules whose `vpnConnectionId` is set from its rule set and re-sends them, read fresh, on every rules replace (`vpcnsgs.MergeVpnRuleBodies`)
- `client.DoOnce` sends the create POSTs for the gateway and the connection, and the connection update PUT. It retries a 429 only, honouring `Retry-After` up to `MaxRetries`, and never sends the request again after a 5xx or a transport error, because a second POST can create a second row, and a second PUT starts a second job
- After a create POST fails, Create adopts only a row this request could have made. It records the request start before the POST and adopts a gateway whose `createdAt` is at most two minutes before that start. A connection also needs a matching name and a `peerAddress` equal to the plan. Any other row produces an error that names the import command, and no state. The lookups run on `context.WithoutCancel`, bounded by the request timeout, so a cancelled apply still finds the row. When a connection lookup fails, the error says a connection may exist. After an adopted row with an `activeJobId`, Create polls that job, best effort, before the final read
- Read stores the private key `job_active` (the row has an `activeJobId`). `ModifyPlan` plans an update that sends the saved settings again only when `desired_applied` is false, the state is not `creating` or `removing`, and `job_active` is false. The warning about unapplied settings shows under the same predicate
- Read stores the private key `update_refused` when a connection failed and its `acceptedVerbs` has no `update`. `ModifyPlan` then plans a replacement
- Delete waits for the 404. The gateway wait covers a failed removal job and a 409 `VPN_GATEWAY_REMOVING`. The connection wait covers a 409 `VPN_STATE_CONFLICT` whose `state` is `removing` with a null `failureReason`. A gateway delete refused as `VPN_GATEWAY_BUSY_CREATING` waits for the create to finish and sends the delete again. A connection delete that parks in `removing` with a failure reason is sent once more, and a second park is reported. Each wait shares one deadline with the job poll
- `gpcn_vpc_nsg` keeps an owned rule in its set when a declared rule copies it exactly, description included, and sends it once. Before a rules replace it refuses a set over 100 rules, and a failed ownership read after the replace is a warning
- Acceptance tests read `GPCN_TEST_VPN_DATACENTER_ID` (a data center that offers site-to-site VPN) and skip when it is unset

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
- **Acceptance tests**: Create real resources, and there are no sweepers, so a failed run leaves them behind. Run with `make testacc`. Run individual tests to iterate faster. The VPN and VPN-rule cases read `GPCN_TEST_VPN_DATACENTER_ID` and build their own VPC and subnet in 10.120.0.0/16 (gateway), 10.121.0.0/16 (connection) and 10.122.0.0/16 (security group). The network case reads `GPCN_TEST_NETWORK_ID` (an existing network the key can see) and skips when it is unset, because the provider refuses to create a `gpcn_network`. The L2 segment case reads `GPCN_TEST_L2_DATACENTER_ID`, and the public IP cases read `GPCN_TEST_VPC_ID` and `GPCN_TEST_VPC_NIC_ID`; the virtual machine cases build their own VPC and subnet, each in a fixed /16 inside 10.112.0.0/12 (second octets 112 to 119; the VPN cases take 120 to 122 of the same /12; the VPC, subnet and security group cases take windows of 16 second octets starting at 64, 80 and 96) with `acknowledge_overlap` set, so a leftover from a failed run does not block the next.
- `TestCase.ErrorCheck` never runs for a step that sets `ExpectError`; assert an error's absence in `ErrorCheck` on a step without `ExpectError`.

## Documentation

`make generate` (`tools/tools.go`) runs `terraform fmt` on `examples/` and then tfplugindocs. `docs/` is generated: never edit it by hand; CI regenerates it and fails on any diff.

- Landing page: `templates/index.md.tmpl` (embeds `examples/provider/provider.tf`).
- Resources and data sources: `templates/resources.md.tmpl` and `templates/data-sources.md.tmpl` are the fallbacks. Their `subcategory:` line is an if/else chain: resources default to Networking (Compute: `gpcn_virtualmachine`, `gpcn_gpu`; Storage: `gpcn_volume`, `gpcn_volume_attachment`; Identity & Access: `gpcn_resource_group`, `gpcn_ssh_key`); data sources default to Data Centers (Compute: images, sizes, gpu_inventory; Identity & Access: resource_groups). Add a new name to the right branch. `templates/resources/<name>.md.tmpl` overrides one page (`network`, `virtualmachine`, `vpc_vpn_gateway`, `vpc_vpn_connection`) and hard-codes its subcategory.
- Examples: `examples/resources/gpcn_<name>/resource.tf` and `import.sh`, `examples/data-sources/gpcn_<name>/data-source.tf`.
- Guides: `templates/guides/*.md.tmpl`, with runnable configs under `examples/guides/<guide>/`.
- Prose follows GPCN Hub style: "GPCN™" (with ™), "data center" (two words; `datacenter_id` and other identifiers keep their names), "security group" (not NSG), "elastic IP" for an address a VPC holds. Schema Descriptions follow the same rule, because they render into docs.

## Code Quality

### Linting

The project uses golangci-lint (`.golangci.yml`). Key enabled linters (also enabled: `misspell`, `nilerr`, `prealloc`, `unconvert`, `unparam`, `wastedassign`):

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

- `test.yml` (PRs and `main`): lint, unit tests (`go test -race` with coverage uploaded to Codecov, terraform installed), build, and a docs-drift check that regenerates docs and fails on any difference.
- `security.yml`: govulncheck and CodeQL, on PRs, `main`, and weekly.
- `release.yml`: goreleaser on `v*` tags, with GPG-signed checksums.
- Dependabot: gomod at `/` and GitHub Actions, both weekly. `tools/go.mod` (terraform-plugin-docs) is not covered; bump it by hand.
- Acceptance tests never run in CI.

## Commits

Read `.claude/rules/commit-conventions.md` before you commit, push, or open a pull request, including a draft pull request.

## Releasing

To prepare a new release:

1. Update `CHANGELOG.md` with the new version and release notes, and replace `(Unreleased)` with the release date
2. On a minor or major release, update the pin `version = "~>X.Y.0"` in every example file under `examples/` (`grep -rln 'version *=' examples` lists them): `resources/gpcn_*/resource.tf`, `data-sources/gpcn_*/data-source.tf`, `provider/provider.tf`, `guides/getting-started/main.tf` and `provider-install-verification/main.tf`; and `README.md` (`~> X.Y`). A patch release needs no pin change.
3. Run `make` to regenerate documentation (this renders `docs/` from templates and examples, including the landing page and guides)
4. Commit all changes
5. Create the tag (`git tag vX.Y.Z`) and ask the user to push it — pushing needs explicit authorization, see `.claude/rules/commit-conventions.md`

## MCP Servers

Always use Context7 when I need library/API documentation, code generation, setup or configuration steps without me having to explicitly ask.
