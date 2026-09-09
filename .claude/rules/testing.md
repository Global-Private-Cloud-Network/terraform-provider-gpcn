---
description: How to write and run unit tests and acceptance tests for resources
paths:
  - "internal/**/*_test.go"
  - "internal/testutil/**"
---

# Testing

## Two kinds of test

- **Unit tests** live in `internal/{resource}/`. They need no credentials. Run them
  with `make test`.
- **Acceptance tests** live in `internal/provider/{resource}_resource_test.go`. They
  create real resources and need credentials. Run them with `make testacc`, or one at
  a time with `make testaccnamed TEST=...`.

## Unit tests

- Mock HTTP with `SetupMockServerWithGpcnClient` and `MockTransport` from
  `internal/testutil/mock_http.go`. Use the `HandleJobResponse` and
  `HandleCreateJobResponse` helpers for the async job endpoints.
- Keep pure mapper tests (`TestMapXxxResponseToModel`) separate from HTTP-flow tests
  (`TestCreateXMockHTTP`).
- In an HTTP-flow test, `switch` on method and path, route each to a handler, and
  assert each endpoint was hit.

## Acceptance tests

- Use `testProtoV6ProviderFactories` and the `providerConfig` prefix from
  `provider_test.go`.
- Write steps that exercise create, import, update, and replace.
- Assert with `TestCheckResourceAttr` and `TestCheckResourceAttrSet`.
- Assert plan actions with `plancheck.ExpectResourceAction` (Create / Update /
  Replace) in `ConfigPlanChecks`.
- Assert state values with `statecheck.ExpectKnownValue` in `ConfigStateChecks`.

## Validator tests

- Test a validator failure with `resource.UnitTest` and
  `ExpectError: regexp.MustCompile(...)`. Validation fails at plan time, so these make
  no API call and need no credentials.
