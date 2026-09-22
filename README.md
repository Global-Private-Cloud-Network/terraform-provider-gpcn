# Terraform Provider for GPCN

[![Terraform Registry](https://img.shields.io/badge/registry-Global--Private--Cloud--Network%2Fgpcn-blue)](https://registry.terraform.io/providers/Global-Private-Cloud-Network/gpcn/latest)
[![Tests](https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn/actions/workflows/test.yml/badge.svg)](https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn/actions/workflows/test.yml)
[![License: MPL-2.0](https://img.shields.io/badge/license-MPL--2.0-brightgreen)](LICENSE)

Manage [Global Private Cloud Network](https://gpcn.com) infrastructure with Terraform: VPCs, subnets, security groups, public IP addresses, L2 segments, virtual machines, GPU instances, volumes, SSH keys and resource groups.

- **Documentation:** every resource and data source, with examples, on the [Terraform Registry](https://registry.terraform.io/providers/Global-Private-Cloud-Network/gpcn/latest/docs).
- **Examples:** runnable configurations under [`examples/`](examples/).
- **Changes:** [CHANGELOG.md](CHANGELOG.md), including upgrade notes between versions.

## Requirements

- Terraform 1.5 or later
- A GPCN account and an API key with the permissions for the resources you manage (each resource's documentation names them)

## Usage

```hcl
terraform {
  required_providers {
    gpcn = {
      source  = "Global-Private-Cloud-Network/gpcn"
      version = "~> 1.4"
    }
  }
}

provider "gpcn" {}

data "gpcn_datacenters" "chicago" {
  name        = "Chicago"
  vpc_capable = true
}

resource "gpcn_vpc" "main" {
  name          = "main"
  datacenter_id = data.gpcn_datacenters.chicago.datacenters[0].id
  cidr          = "10.10.0.0/16"
}

resource "gpcn_vpc_subnet" "app" {
  vpc_id = gpcn_vpc.main.id
  name   = "app"
  cidr   = "10.10.1.0/24"
}
```

### Authentication

The provider reads its credentials from the environment:

```bash
export GPCN_HOST="https://api.gpcn.com"
export GPCN_API_KEY="..."
```

Both can also be set as `host` and `api_key` in the `provider "gpcn"` block. Keep the key out of configuration files that are committed to version control.

### Timeouts and retries

Long-running operations (creating machines, volumes and networks) are polled until they finish. The provider block accepts `request_timeout`, `polling_timeout` and `max_retries`; see the [provider documentation](https://registry.terraform.io/providers/Global-Private-Cloud-Network/gpcn/latest/docs) for defaults.

## Upgrading

Read the entry for the new version in [CHANGELOG.md](CHANGELOG.md) before you upgrade. Minor releases can require configuration changes; the changelog says which, and how to migrate existing state.

## Getting help

- Something does not work as documented: open a [bug report](https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn/issues/new?template=bug_report.yml).
- The provider is missing something: open a [feature request](https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn/issues/new?template=feature_request.yml).
- A security problem: follow [SECURITY.md](SECURITY.md). Please do not open a public issue.

## Contributing

Contributions are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers the development setup, how to run the unit and acceptance tests, the commit conventions and the review process. Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development

```bash
make            # format, lint, build and install into your GOBIN, regenerate docs
make test       # unit tests, no credentials needed
make testacc    # acceptance tests: create real resources against GPCN_HOST
```

To run your local build, add a dev override to `~/.terraformrc` pointing `gpcn.com/dev/gpcn` at your `GOBIN` and use that source in the configuration under test. Details in [CONTRIBUTING.md](CONTRIBUTING.md#development-setup).

## License

[Mozilla Public License 2.0](LICENSE)
