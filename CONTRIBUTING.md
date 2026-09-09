# Contributing

This guide gets a new contributor productive. It links to the deeper docs rather than
repeating them.

## Setup

Follow the local development steps in the [README](README.md): install Go and
Terraform, run `go mod tidy && go install .`, and create `~/.terraformrc` with dev
overrides for `gpcn.com/dev/gpcn`. Set the two required environment variables:

- `GPCN_API_KEY` — API key for authentication.
- `GPCN_HOST` — base URL for the GPCN API.

## The `make` workflow

```bash
make          # fmt, lint, install, generate (the default)
make build    # build only
make lint     # lint — run before you commit
make test     # unit tests, no credentials needed
make generate # rebuild docs/ from schema descriptions + examples/
make testacc  # acceptance tests — creates real resources, needs credentials
make testaccnamed TEST=TestAccNetworkResource_basic  # one acceptance test
```

Run `make lint` and `make test` before every commit.

## Where things live

| Path | Holds |
| ---- | ----- |
| `internal/provider/` | Terraform framework glue: schemas, registration, thin CRUD. |
| `internal/{resource}/` | Per-resource API logic: requests, responses, mappers. |
| `internal/client/` | Shared HTTP client, retry, and async job polling. |
| `docs/` | **Generated** reference. Do not hand-edit — `make generate` overwrites it. |
| `examples/` | Example `.tf` files. `make generate` pulls them into `docs/`. |
| `docs-for-developers/` | The developer docs below. |

## Read next

- [docs-for-developers/architecture.md](docs-for-developers/architecture.md) — how the
  provider is built. Read this first.
- [docs-for-developers/adding-a-resource.md](docs-for-developers/adding-a-resource.md)
  — the checklist for a new resource.

## Commits, branches, and pull requests

The authority is [.claude/rules/commit-conventions.md](.claude/rules/commit-conventions.md).
In short:

- Never commit or push to `main`. Branch as `<type>/<short-description>`, e.g.
  `fix/grpc-oom-cve`. Open a pull request into `main`.
- Write commit messages and PR titles in Conventional Commits format:
  `<type>(<scope>): <subject>`. The subject is imperative, lowercase, and 72
  characters or less.
- `main` is squash-merged, so the PR title becomes the commit subject on `main`.

## Documentation style

Write docs and code comments in the house style. See
[.claude/rules/comment-style.md](.claude/rules/comment-style.md): brief, load-bearing,
ASD-STE100 Simplified Technical English. State the non-obvious reason. Do not restate
the code.
