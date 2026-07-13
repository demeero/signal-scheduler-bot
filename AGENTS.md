# AGENTS.md

# Signal Scheduler Bot

This repository contains a small Go service that lets a single user schedule Signal messages by sending commands to their own `Note to Self` chat.

The service depends on:

- `signal-rest-api` for receiving commands, resolving recipients, and sending messages
- `bbolt` for storing scheduled messages and delivery state

User-facing setup and usage belong in [`README.md`](/Users/demeero/workspace/signal-scheduler-bot/README.md:1). Keep `AGENTS.md` focused on contributor guidance and implementation boundaries.

## Project Intent

- Keep the project small, explicit, and easy to modify.
- Prefer straightforward code over abstractions.
- Do not introduce DDD, Hexagonal Architecture, Clean Architecture, CQRS, Event Sourcing, or similar enterprise patterns.
- Do not add web UI, multi-user flows, or generic automation-platform concepts unless explicitly requested.

## Implementation Shape

The current codebase is intentionally compact and centered around a few packages:

- `cmd/botsrv` wires configuration, logging, BoltDB, and workers.
- `internal/bot` handles command polling and parsing.
- `internal/outbox` stores scheduled messages and drives delivery attempts.
- `internal/signaladapter` wraps `signal-rest-api`.
- `internal/config`, `internal/logbrick`, and `internal/errbrick` provide shared support code.

At runtime, the service currently runs periodic loops for:

- inbound polling
- due-message sending
- outbox vacuuming

Keep new code aligned with this simple shape unless there is a clear reason to change it.

## Behavioral Boundaries

- The bot is controlled through `Note to Self`.
- Command parsing should stay strict and return descriptive errors.
- Scheduled messages are persisted in UTC.
- User-facing date parsing and formatting use the configured timezone.
- Recipient resolution should continue to happen through Signal contacts before scheduling.
- Delivery state should stay explicit and easy to inspect in storage and logs.

When changing behavior, prefer updating README for user-visible changes instead of growing `AGENTS.md` into product documentation.

## Working Agreements

- Start with a short plan, then execute.
- All comments and documentation should be in English.
- Add or adjust tests when behavior changes. If tests are not feasible, explain why and what was validated instead.
- When you introduce non-obvious behavior, leave a short comment or update the nearest README/AGENTS.md. Keep docs concise.
- After changes, check whether the project still builds and whether the relevant tests pass.
- Fix root causes, not symptoms.

## Code Style

- Keep functions flat. Prefer guard clauses and early returns over nested control flow.
- Prefer small packages and small files.
- Keep code explicit and readable.
- Use singular, lowercase package names.
- Export only identifiers that are needed outside the package.
- Keep config, logging, and naming consistent with the existing code.

## Error Handling

- Use shared sentinels from `internal/errbrick/errors.go` for cross-layer error classification where appropriate.
- Wrap low-level errors with context using `fmt.Errorf("...: %w", err)`.
- Do not swallow infrastructure errors.
- Do not panic except during startup-level failures where the process cannot continue.

## Testing

- Prioritize tests for parser behavior, scheduling behavior, storage state transitions, and Signal adapter behavior.
- Prefer real BoltDB usage with temporary files over mocking storage.
- Avoid excessive mocking in general.
- Keep tests close to exported behavior.

## Keep It Simple

Before adding code, ask:

- Can this be solved with the standard library?
- Can this be done without another abstraction?
- Can this be understood quickly by someone new to the repo?

If yes, prefer that solution.
