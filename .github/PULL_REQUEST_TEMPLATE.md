## What changed

<!-- One paragraph. What does the provider do differently after this change? -->

## Why

<!-- The problem, the issue number if there is one, and why this is the right fix. -->

## How it was tested

- [ ] `make test` passes locally
- [ ] `make lint` passes locally
- [ ] Acceptance tests that touch this change were run against a non-production environment: <!-- list them, or say none apply -->
- [ ] New behaviour has a test that fails without the change

## Checklist

- [ ] The pull request title is a Conventional Commit subject (`type(scope): imperative summary`, 72 characters or fewer); it becomes the commit on `main`
- [ ] `CHANGELOG.md` has a line under the unreleased version
- [ ] Docs were regenerated with `make generate` if a description or example changed
- [ ] User-facing strings that changed are pinned by a test
