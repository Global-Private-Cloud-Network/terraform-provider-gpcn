---
description: How to structure a resource or data source, from package layout to provider registration
paths:
  - "internal/**/*"
---

# Resource Authoring

The full walkthrough is `docs-for-developers/adding-a-resource.md`. This rule is the
directive summary. Follow both.

## Package layout

- One package per resource under `internal/{resource}/`. Use these files:
  `resource_model.go`, `crud_actions.go`, `constants.go`, `errors.go`, `logging.go`,
  and, when needed, `validators.go` and `plan_modifiers.go`.
- Put the `ResourceModel` struct, the API response struct, and `MapXxxResponseToModel`
  in `resource_model.go`. Put the API calls in `crud_actions.go`.
- Copy `networks` for the shape. It is the canonical example.

## Provider layer

- Schemas live in `internal/provider/{resource}_resource.go`, never in the resource
  package.
- Keep the provider `Create`/`Read`/`Update`/`Delete` methods thin. Set the
  correlation ID, read plan or state into the model, call the package function, map
  the response back, and set state.
- Register every new resource in the `Resources()` slice, and every data source in the
  `DataSources()` slice, in `internal/provider/provider.go`. Terraform does not see it
  otherwise.
- Add interface-assertion vars at the top of the file
  (`var _ resource.Resource = &xResource{}`).

## Rules

- Write a clear `Description` for every schema attribute. The descriptions become the
  generated user docs.
- Never put inline error or log strings in resource code. Use the `ErrSummary*` /
  `ErrDetail*` constants in `errors.go` and the `Log*` constants in `logging.go`.
- Never discard the `diag.Diagnostics` from `types.*ValueFrom()`. On an error, set the
  field to a Null value as the fallback.
- Unexport package-internal struct fields.
- Every `//nolint:gosec` carries a `G###` code and a reason.
- After a schema or example change, run `make generate` and confirm the `docs/` page.
