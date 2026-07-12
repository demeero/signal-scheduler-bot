# AGENTS.md

# Signal Scheduler

A small Go application that adds scheduled messages to Signal by using `signal-rest-api`.

## Goal

The application allows a single user to schedule Signal messages by sending commands to their own **Note to Self** chat.

The application is intentionally simple.

Do **not** introduce DDD, Hexagonal Architecture, Clean Architecture, CQRS, Event Sourcing, or other enterprise patterns.

Prefer readable and maintainable code over abstraction.

---

# Stack

- Go (latest stable)
- bbolt
- signal-rest-api

---

# Project Philosophy

This project is intended to be:

- small
- easy to understand
- easy to debug
- easy to modify

When making architectural decisions, always choose the simpler solution unless complexity is clearly justified.

---

# High Level Architecture

The application consists of several independent components.

```
main
 ├── signaladapter
 ├── dbadapter (bbolt)
 ├── inbound (inbound messages processing)
 ├── outbound (outbound messaging processing)
```

---

# Workers

There are exactly two long-running workers.

## 1. Command Worker

Responsibilities:

- poll Note to Self messages
- parse commands
- validate commands
- create scheduled messages
- list scheduled messages
- cancel scheduled messages
- reply back to Note to Self with success/error

This worker **never sends scheduled user messages**.

---

## 2. Scheduler Worker

Runs periodically.

Responsibilities:

- scan BoltDB
- find due messages
- send messages through signal-rest-api
- update message status
- retry temporary failures
- notify Note to Self when permanent failure occurs

This worker never parses commands.

---

# Storage

Use bbolt as the only database.

Keep the schema simple.

No migrations are required.

Database schema should be easy to evolve manually.

---

# Command Parsing

Supported commands:

```
/schedule YYYY-MM-DD HH:mm +380XXXXXXXXX Message

/schedule tomorrow HH:mm +380XXXXXXXXX Message

/schedule today HH:mm +380XXXXXXXXX Message

/schedule YYYY-MM-DD HH:mm "Contact Name" Message

/schedule tomorrow HH:mm "Contact Name" Message

/schedule today HH:mm "Contact Name" Message

/upcoming

/cancel MESSAGE_ID

/help
```

Parsing should be strict.

Return descriptive errors.

Do not silently ignore invalid input.

---

# Contact Resolution

Recipients may be:

- phone number
- Signal contact name

Before scheduling:

- verify the contact exists
- resolve the Signal recipient identifier
- reject unknown contacts

Store the resolved identifier together with the original recipient string.

---

# Scheduler

Scheduler periodically scans pending messages.

For every message:

- skip cancelled
- skip already sent
- send when ScheduledAt <= now

After successful send:

- mark as sent

After temporary failure:

- retry later

After permanent failure:

- mark failed
- notify Note to Self

---

# Retry Policy

Retry only temporary failures.

Suggested defaults:

- max retries: 3
- retry delay: 3 minutes

Do not retry permanent errors.

---

# Logging

Use structured logging.

Log:

- worker start
- worker stop
- parsed commands
- scheduled messages
- sent messages
- retries
- failures

Avoid excessive logging.

---

# Error Handling

Return wrapped errors.

Prefer:

```go
fmt.Errorf("parse command: %w", err)
```

Do not panic except during startup.

---

# Concurrency

Workers run independently.

BoltDB access must be safe.

Avoid unnecessary goroutines.

---

# Time

Store all timestamps in UTC.

Convert user input into UTC before saving.

---

# IDs

Message IDs should be unique and human-friendly enough to be used with:

```
/cancel MESSAGE_ID
```

Simple increasing uint64 IDs are preferred.

---

# Code Style

Prefer small packages.

Prefer small files.

Avoid files larger than ~500 lines.

Avoid functions larger than ~100 lines.

Keep code explicit.

Do not introduce interfaces unless there are at least two implementations or testing clearly benefits.

---

# Testing

Prioritize tests for:

- command parser
- date parsing
- scheduler logic
- retry logic
- storage

Avoid excessive mocking.

Prefer real BoltDB using temporary files.

---

# Keep It Simple

When adding new code, ask:

- Can this be solved with the standard library?
- Can this be done without another abstraction?
- Can this be understood in one minute?

If yes, prefer that solution.

The simplest correct implementation is usually the preferred implementation.

## Working agreements

- Start with a short plan (2–6 bullets), then execute. If unsure, ask at most 1–2 clarifying questions or propose 1–2 options with tradeoffs.
- All comments and documentation should be in English.
- Add or adjust tests when behavior changes. If tests aren’t feasible, explain why and what was validated instead.
- When you introduce non-obvious behavior, leave a short comment or update the nearest README/AGENTS.md. Keep docs concise.
- After changes, check whether the project still builds and whether the relevant tests pass.
- Fix root causes, not symptoms. Do not stop at suppressing errors or adding defensive conditionals without addressing the underlying cause.

## Coding Style & Naming Conventions

- Keep functions flat. Prefer guard clauses and early returns over nested control flow.
- Use singular names for packages.
- Package names are short, lowercase, and business-oriented.
- Go packages should be self-contained and have minimal external dependencies.
- Export only identifiers that are needed outside the package.
- Go 1.26 module: prefer modern Go features that fit naturally.
- Indentation follows Go defaults (tabs in Go files).
- Keep CLI/config/logging naming consistent with existing patterns.

### Error conventions

- Use shared sentinels from `internal/errbrick/errors.go` for cross-layer error classification.
- Wrap low-level errors with context using `fmt.Errorf(...: %w, err)`.
- Do not swallow infrastructure errors.

## Testing Guidelines

- Use `testify` for assertions and suites.
- Prefer `require` for checks after which continuing the test would produce misleading follow-up failures.
- For error assertions, prefer `require.Error`, `require.NoError`, `require.ErrorIs`, `require.ErrorContains`, and `require.ErrorAs`.
- Name tests `Test<ElementName>[_AdditionalInfo]`.
- Keep fixtures under `testdata/` where applicable.
- Prefer table tests, but split them into separate tests when tables become harder to read than the behavior they cover.
- Prefer testing exported behavior over testing private helpers directly.
- Do not use `t.Parallel()` without approval.
- Use `t.Context()` instead of `context.Background()` in tests.
- If a mock is required, use `go:generate mockgen ...` and place generated mocks in a `mock` directory.
