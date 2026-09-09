# Developer docs

These docs explain how this provider is built and how to extend it. They are for
people who work on the provider, not for people who use it.

## Reading order

1. [architecture.md](architecture.md) — the layers, the file layout, and the async
   request flow. Start here.
2. [adding-a-resource.md](adding-a-resource.md) — the step-by-step checklist for a new
   resource or data source.
3. [../.claude/rules/testing.md](../.claude/rules/testing.md) — the testing
   conventions.

## Scope

- **Not covered:** Go and Terraform Plugin Framework basics. The work runs through an
  AI that already knows both. These docs stay specific to this project.
- **Reference lives elsewhere:** per-resource attributes, imports, and example HCL are
  generated into `docs/` and published to the Terraform Registry. Do not duplicate or
  hand-edit that — `make generate` overwrites `docs/`.

## Diagrams

Diagram sources and the render command are in [diagrams/](diagrams/). Edit the `.mmd`
source, re-render the `.svg`, and commit both.
