# Contributing to the GPCN Terraform provider

Thank you for helping improve the provider. This page covers how to report a problem, propose a change, and get a pull request merged.

## Before you start

- Using the provider, not changing it? Start with the [provider documentation on the Terraform Registry](https://registry.terraform.io/providers/Global-Private-Cloud-Network/gpcn/latest/docs) and the examples under `examples/`.
- Found a bug or want a feature? Open an issue first using the templates. A short reproduction (the Terraform block, the command, and the error text) saves a round trip.
- Security problems go through [SECURITY.md](SECURITY.md), not a public issue.

## Development setup

You need Go (see `go.mod` for the version), Terraform 1.5 or later, and `make`.

```bash
git clone https://github.com/Global-Private-Cloud-Network/terraform-provider-gpcn
cd terraform-provider-gpcn
make            # format, lint, install the provider into your GOBIN, regenerate docs
```

To run your local build against real configurations, point Terraform at it with a dev override in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "gpcn.com/dev/gpcn" = "<output of: go env GOPATH>/bin"
  }
  direct {}
}
```

and use `source = "gpcn.com/dev/gpcn"` in the configuration you are testing. The provider reads `GPCN_HOST` and `GPCN_API_KEY` from the environment.

## Tests

| Command | What it runs |
| --- | --- |
| `make test` | Unit tests. No credentials. Needs a `terraform` binary on `PATH`, or the test framework downloads one. |
| `make testacc` | Acceptance tests. They create and destroy real resources against the API named by `GPCN_HOST` and cost money and time. Never run them against production. |
| `make testaccnamed TEST='TestVpcResource$'` | One acceptance test. Anchor the name with `$`; the `-run` pattern is a regular expression. |
| `make lint` | golangci-lint with the repository configuration. |

Every behaviour change comes with a test that fails without it. Plan-level behaviour (what `terraform plan` proposes, which diagnostics appear) is tested by driving real Terraform against an `httptest` mock of the API; see the `*_plan_test.go` files for the pattern. User-facing strings (error messages, schema descriptions) are pinned byte for byte by tests, because they are the provider's contract with its users.

Acceptance tests do not run in CI. Run the ones that touch your change locally before you open the pull request, and say in the description which ones you ran.

## Making a change

1. Branch from `main`. Name the branch `<type>/<short-description>`, for example `fix/subnet-import-prefix`.
2. Keep the change focused. One fix or one feature per pull request.
3. Regenerate the documentation if you changed a schema description or an example: `make generate`. CI fails on any drift between the templates and `docs/`.
4. Add a line to `CHANGELOG.md` under the unreleased version.
5. Run `make` and `make test` before you push.

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <subject>

<optional body>
```

Types: `feat`, `fix`, `deps`, `ci`, `docs`, `test`, `refactor`, `chore`. The subject is imperative, lower case, at most 72 characters, no trailing period. The body explains why, wrapped at 72 columns. `main` is squash-merged, so the pull request title becomes the commit subject on `main`; write it in the same format.

## Pull requests

- Fill in the pull request template. Say what changed, why, and how you tested it.
- One approving review is required. Reviews here are thorough: expect questions about edge cases, error text, and what happens when the API refuses a request.
- Keep the branch rebased on `main`; do not merge `main` into it.
- Do not add generated attribution lines or tool credits to commits or descriptions.

## Style

- Go code is `gofmt`-clean and passes `make lint`. Handle every returned diagnostic; never discard one with `_`.
- Comments explain why, in the present tense, in short sentences.
- Error messages tell the user what happened and what to do next, in the API's own words where the API refused something.

## Licence

By contributing you agree that your contributions are licensed under the [Mozilla Public License 2.0](LICENSE), the licence of this repository.
