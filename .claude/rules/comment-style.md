---
description: Describes when a comment is allowed to exist and what criteria it must follow to justify its existence
paths:
  - "internal/**/*"
---

# Comment Style

## A comment is a last resort

Express meaning in the code first. Use a clear name, a small function, or a named
constant. Write a comment only when the code cannot carry the meaning on its own.

Packages under `internal/` have no consumers outside this module. Exported
identifiers here do not need a doc comment for API documentation.

## Never write an essay

Never write an ADR-length comment. A long comment rots as the code moves past it,
and a stale comment is worse than no comment. Put design history and rejected
alternatives in the pull request or in an issue, not in the source.

## Every comment must earn its place

Each comment that survives must pass both tests:

- It gives a non-obvious _why_. It does not restate what the code does.
- It carries unique value. It does not repeat a name, a type, or a nearby comment.

Delete a comment that fails either test. Judge each comment on its own. A block of
comments is not acceptable because the block reads well together.

## Language

Write every comment in ASD-STE100 Simplified Technical English:

- Use the active voice. Write "the poller stops", not "the poller is stopped".
- Give one idea in each sentence. Keep a sentence to 20 words or less.
- Use the present tense.
- Use one word for one meaning. Use the same term for the same thing in each package.
- Use the simple word when one exists. Write "use", not "utilize".
- Write complete sentences. Do not remove the articles.

## Length

- 10 lines is the hard limit for one comment.
- Over 5 lines, validate the comment before you keep it. Ask whether the code can
  carry the meaning instead. Keep the long form only when the answer is clearly no.

A comment that needs more than 10 lines signals unclear code. Fix the code.
