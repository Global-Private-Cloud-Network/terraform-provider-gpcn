# Adding a resource

Every resource follows the same pattern. Learn `networks` and you can build any of
them. This document is the checklist. Copy the shape from `internal/networks/` and
`internal/provider/networks_resource.go` as you go.

Read [architecture.md](architecture.md) first for the layers and the async flow.

## Before you start

Pick the API endpoints for create, read, update, and delete. Note which calls return
a job ID (async) and which return the object directly. Most create/update/delete calls
are async.

## Step 1 — Create the resource package

Add `internal/{resource}/` with these files. Copy `networks` and rename.

| File | What goes in it |
| ---- | --------------- |
| `resource_model.go` | The `ResourceModel` struct, the API response struct, and `MapXxxResponseToModel`. |
| `crud_actions.go` | `CreateX` / `GetX` / `UpdateX` / `DeleteX`. |
| `constants.go` | The endpoint base URL and any domain constants. |
| `errors.go` | `ErrSummary*` and `ErrDetail*` constants. |
| `logging.go` | `Log*` constants. |
| `validators.go` | Custom validators. Add only if you need them. |
| `plan_modifiers.go` | Custom plan modifiers. Add only if you need them. |

## Step 2 — Define the model and the mapper

In `resource_model.go`:

- Define `ResourceModel` with framework value types (`types.String`, `types.List`,
  ...) and `tfsdk:` tags that match the schema attribute names.
- Define the API response struct with `json:` tags that match the API.
- Write `MapXxxResponseToModel(ctx, response, model) ResourceModel`. It copies the
  response into the model. When you use `types.*ValueFrom`, **never discard the
  returned `diag.Diagnostics`** — on an error, set the field to a Null value as the
  fallback. See `internal/networks/resource_model.go` for the pattern.

## Step 3 — Write the API calls

In `crud_actions.go`, follow the async flow for `CreateX` / `UpdateX` / `DeleteX`:

1. Log with a `logging.go` constant.
2. Build the request body as `map[string]any` and `json.Marshal` it.
3. `http.NewRequestWithContext(ctx, method, url, body)` — always with the context.
4. `response, err := gpcnClient.DoWithRetry(request)`, then `defer response.Body.Close()`.
5. Unmarshal into `client.JobStatusSingularResponse` to read the job ID.
6. `client.PerformLongPolling(gpcnClient, ctx, "action label", jobID)`.
7. `client.GetJobResourceID(jobResp)` for the new ID.
8. Call `GetX` for the full object and return it.

`GetX` is a plain `GET` plus unmarshal, with no polling. Do not put inline strings in
these functions — use the `errors.go` and `logging.go` constants.

## Step 4 — Add the schema file

Add `internal/provider/{resource}_resource.go`. Copy `networks_resource.go`. It needs:

- Interface assertions:
  ```go
  var (
      _ resource.Resource                = &xResource{}
      _ resource.ResourceWithConfigure   = &xResource{}
      _ resource.ResourceWithImportState = &xResource{}
  )
  ```
- `NewXResource()` constructor that returns `&xResource{}`. The struct holds
  `client *client.GpcnClient`.
- `Metadata`: `resp.TypeName = req.ProviderTypeName + "_x"`. This name sets the
  `gpcn_x` type used in HCL.
- `Schema`: the attribute map. Set Required/Optional/Computed, `Validators`,
  `PlanModifiers`, and `Default`. Write a clear `Description` for every attribute —
  **the descriptions become the user documentation.**
- `Configure`: type-assert `req.ProviderData.(*client.GpcnClient)` with the nil guard
  and the `!ok` error, exactly as `networks_resource.go` does.
- `Create` / `Read` / `Update` / `Delete`: keep them thin. Set the correlation ID,
  read the plan or state into the model, call the package function, map the response
  back with `MapXxxResponseToModel`, and set state. Accumulate diagnostics with the
  `resp.Diagnostics.Append(...); if resp.Diagnostics.HasError() { return }` pattern.
- `ImportState`: `resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)`.

## Step 5 — Register the resource

Add the constructor to the `Resources()` slice in `internal/provider/provider.go`.
Terraform does not see the resource until you do this. A data source goes in the
`DataSources()` slice instead.

## Step 6 — Add examples

Add `examples/resources/gpcn_{resource}/resource.tf` and `import.sh`. `make generate`
pulls these into the generated docs. Follow the layout of an existing example folder.

## Step 7 — Write tests

- **Unit tests** in `internal/{resource}/`: mapper tests with no HTTP, plus HTTP-flow
  tests with `SetupMockServerWithGpcnClient`. Run with `make test`. No credentials.
- **Acceptance tests** in `internal/provider/{resource}_resource_test.go`: real
  create/import/update/replace steps. Run with `make testacc`. Needs credentials.

See [../.claude/rules/testing.md](../.claude/rules/testing.md) for the conventions.

## Step 8 — Generate docs and check

```bash
make generate   # rebuilds docs/ from schema descriptions + examples/
make            # fmt, lint, install, generate
make test       # unit tests
```

Confirm the new page appears under `docs/resources/`. Confirm `make lint` passes.
