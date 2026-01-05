# Agent Self-Correction Advice

This document contains advice generated from past session reflections to improve future
interactions and code modifications.

## General Principles

*   **Prioritize Robust File Modifications:** When modifying code, especially Go files:
    *   **Prefer full file rewrites (using `write_file` without `insert_after_line` or `insert_before`) for
        complex or multi-line changes.** This avoids accidental corruption due to incorrect line numbering,
        whitespace, or partial overwrites.
    *   If using `replace`, ensure `old_string` and `new_string` are precise, including full context
        (e.g., 3 lines before/after) to prevent unintended side effects, and consider
        `expected_replacements` for multiple matches.
*   **Proactive Test Best Practices:** When writing tests or implementing new features:
    *   **Always include error handling for `io.Closer` implementations (like `Close()` methods) within
        `defer` statements in tests.** Use the pattern:
        ```go
        defer func() {
            if err := obj.Close(); err != nil {
                t.Errorf("Close() returned an unexpected error: %v", err)
            }
        }()
        ```
    *   Proactively address potential concurrency issues (race conditions, deadlocks) with appropriate Go
        primitives (`sync.Mutex`, `sync.RWMutex`, channels, contexts) and clearly explain the rationale,
        referencing the Go Memory Model where applicable.
*   **Design for Testability:** When creating new components or functions, actively consider how they will be tested.
    *   For dependencies like `database/sql`, encourage dependency injection (e.g., by accepting interfaces)
        to facilitate mocking.
    *   If a method returns a standard library concrete type that is hard to mock (e.g., `*sql.Row`),
        consider wrapping it in a custom interface (e.g., `rowScanner`) to make mocking possible without
        modifying standard library types.
*   **Verify After Every Change:** After any code modification, always run relevant tests (`go test -v ./...`)
    and, if concurrency is involved, also with the race detector (`go test -race -v ./...`).
*   **Confirm Scope:** If a request is open-ended (e.g., "add missing tests") and affects multiple
    files/packages, explicitly clarify the scope with the user (e.g., "Should I also cover `service.go`
    in addition to `component.go`?"). This avoids misinterpreting implicit intentions.
*   Do not include "src/" in the module names.


# General Rules
1. Always base your implementation on the best way to do the healthcheck:
   - Issue at least one search in Google to find out ways to do the health check,
   - Evaluate the methods based on impact analysis -- those with least privileges AND no impact on other user data come first always.
   - Once a method is selected, make sure to implement it in the library.
2. Do always verify your work by running tests.
3. Performance tests should be run explicitly to ensure correct behaviour. Make sure to implement them in such a way that using "go test" or "go bench" is enough.
4. Do not leave code with warnings, errors and hints on. Make sure to fix all compiler suggestions.
5. Watch and fix all warnings like "Unused parameter", "Exported method with unexported return type", "Potential nil dereference" and others. After every fix, compile, format and vet the code. Repeat as many times as needed until the code is clean.
6. For every library, prefer the official drivers over unofficial ones.
7. For every library, create a README.md file, which contains up-to-date instructions on:
   - What health check it provides, and how to use the component.
   - Provide general SLA data for the component (supported calls per second, etc.).
8. Add the core logic:
   - It should keep the current status always available in Health(), and do the health check logic in separate goroutine(s).
   - It should follow the pattern in @src/builtin/uptime.go and @src/postgres/checker.go.
9. Every library must be present in the `go.work` file. 
10. If the library requires CGO support, make sure to note this important observation in the documentation as well.
11. Each Checker component must provide a clear, simple interface for initialization.
12. It is of extreme importance that each checker provides means to handle all errors arising in all spawned goroutines. It should be rock-solid, and in no case should it allow for undefined behavior, unhandled errors and stalled processes.
13. In every case that matters, tasks started by the Checker components must ensure a configurable time budget for execution. For example, if an HTTP call is made, a maximum response wait time must be provided in an Options (passed to the factory method), so that the routine does not stall indefinitely. Same for all other external calls. Utilize contexts with timeout aggressively.
14. Always put time limits on the operations. Do not allow them to run forever.
15. Always update the global README.md file if any changes are made that necessitate it (e.g., adding a new component, changing usage patterns).

# Error handling
- Use `errors.Join()` instead of wrapping errors in `Errorf()`.

# Testing Rules

Always use `assert.*` and `require.*` instead of `if X {t.Fail()}` or equivalents.
Every Component must support at least 200 calls to the `Health()` method per second.
Create integration tests with 100% test coverage (instead of just using unit tests). Use Testcontainers (prefer the specialized over the generic Testcontainer for each component that does require it.).
Always add fuzz tests.
Never assume the code works correctly. Do test extensively with `go test -race` and appropriate tests.
Never assume code coverage. Do test extensively with `go test -cover`.
Never allow tests to run forever. Add sensible time limits to all operations.
Never let a test run for more than 2 minutes. Always embed the test time limits in the setup routines.
Never use `context.Background()`. Always use a context with a timeout, which derives from `t.Context()`.


# Factory Function Rules
Think very carefully of how you design your interfaces, especially the New* factory functions. Your factory functions:
- should only create interfaces that are used for testing (mocking), or 
- the official way to create a client (like, accepting a connection string, for example), or 
- accept a user-provided client instances, where it makes sense.

The general rule is to always search for examples and provide the minimum needed footprint to allow for the majority (90+%) of the cases.

Good examples include:

```go
import "github.com/hivanov/go-healthcheck/src/core"

func NewPostgresHealthcheck(connectionString string) (core.Component, error) { /* implementation goes here */ }
``` 

or

```go
import (
    "github.com/hashicorp/vault/api"
    "github.com/hivanov/go-healthcheck/src/core"
)
func NewVaultHealthcheck(client *api.Client)
```

## Specific Learnings from this Session

*   **`write_file` with `insert_before`/`insert_after_line` can be unreliable.** Avoid using it for anything
    beyond single-line, very localized changes. For anything complex, prefer overwriting the whole file.
*   **Be meticulous about test helper duplication and import cleanup.** Ensure helper functions are
    uniquely defined and unused imports are removed to prevent build errors.
*   **The initial `dbClient` interface for `PostgresChecker` was suboptimal for mocking.** Changing it to
    use `rowScanner` was a good, idiomatic fix that should have been considered earlier during the design phase.
