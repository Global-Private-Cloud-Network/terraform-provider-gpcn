---
description: How to call the GPCN API through the shared client, including retries and async job polling
paths:
  - "internal/**/*"
---

# Client and Async Calls

The narrative is in `docs-for-developers/architecture.md` under "The async request
flow". This rule is the directive summary.

## Every request

- Build requests with `http.NewRequestWithContext(ctx, ...)`. Never build a request
  without the context — the `noctx` linter blocks it.
- Send with `gpcnClient.DoWithRetry(request)`. It adds auth, the correlation ID, and
  the host, and it retries `5xx` and `429` with backoff.
- Add `defer response.Body.Close()` immediately after the send. The `bodyclose` linter
  enforces it.
- Set the correlation ID as the first line of each CRUD operation:
  `ctx = client.WithCorrelationID(ctx)`.

## Async create, update, and delete

- These calls return a job ID. Unmarshal into `client.JobStatusSingularResponse`.
- Poll with `client.PerformLongPolling(gpcnClient, ctx, "action label", jobID)`.
- Read job fields with the bounds-checked `client.GetJobResourceID` and
  `client.GetJobID`. Do not index the job slices directly.
- After the job completes, do a final `GET` for the full object and return it.

## Read and delete

- `GetX` is a plain `GET` with no polling.
- In the provider `Read` and `Delete`, treat `client.IsNotFound(err)` as drift or a
  completed delete. In `Read`, call `resp.State.RemoveResource(ctx)`.

## Errors and diagnostics

- Wrap errors with `%w` when you return them up the stack.
- In the provider layer, accumulate diagnostics with
  `resp.Diagnostics.Append(...); if resp.Diagnostics.HasError() { return }`.
- Convert package errors to diagnostics with the `errors.go` constants.
