---
description: Describes commit message and PR conventions
---

# Commit Conventions

## Authorization

Never commit, push, or open a pull request on your own. The user must ask for each
of these actions:

- Do not run `git commit` until the user asks for a commit.
- Do not run `git push` until the user asks for a push.
- Do not open a pull request until the user asks for one.

Approval covers one action only. Approval to commit is not approval to push.
Approval to push is not approval to open a pull request. Ask again for each step.

When the work is ready, stop and report what you changed. Offer the next command.
Let the user run it or ask you to run it.

## Message format

Write every commit message in Conventional Commits format:

```
<type>(<optional scope>): <subject>

<optional body>
```

Use one of these types:

| Type       | Use for                                           |
| ---------- | ------------------------------------------------- |
| `feat`     | A new feature                                     |
| `fix`      | A bug fix                                         |
| `deps`     | A dependency update (Dependabot uses this prefix) |
| `ci`       | A change to workflows or automation               |
| `docs`     | A documentation change                            |
| `test`     | A test change                                     |
| `refactor` | A change that does not alter behavior             |
| `chore`    | Maintenance that no other type covers             |

Rules for the subject:

- Write the subject in the imperative mood. Example: `add`, not `added`.
- Start the subject with a lowercase letter. Do not end it with a period.
- Keep the subject to 72 characters or less.

Rules for the body:

- Give the reason for the change. Do not restate the diff.
- Wrap the body at 72 characters.

`main` is squash-merged, so the pull request title becomes the commit subject on `main`. Write pull request titles in this same format.

## Attribution

Never add attribution for Claude, Claude Code, or any other AI agent:

- Never add a `Co-Authored-By` trailer to a commit message.
- Never add a "Generated with Claude Code" line, or any equivalent tool credit, to a pull request description.

## Branching

- Never commit to `main`. Never push to `main`. Branch protection also blocks this.
- Create a branch for every change. Name it `<type>/<short-description>`. Example: `fix/grpc-oom-cve`.
- Open a pull request to merge the branch into `main`.
