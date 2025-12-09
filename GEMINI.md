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

# Error handling
- Use `errors.Join()` instead of wrapping errors in `Errorf()`.

# Testing Rules

Always use `assert.*` and `require.*` instead of `if X {t.Fail()}` or equivalents.

# Factory Function Rules
Think very carefully of how you design your interfaces, especially the New* factory functions. Your factory functions:
- should only create interfaces that are used for testing (mocking), or 
- the official way to create a client (like, accepting a connection string, for example), or 
- accept a user-provided client instances, where it makes sense.

The general rule is to always search for examples and provide the minimum needed footprint to allow for the majority (90+%) of the cases.

Good examples include:

```go
import "healthcheck/core"

func NewPostgresHealthcheck(connectionString string) (core.Component, error) { /* implementation goes here */ }
``` 

or

```go
import (
    "github.com/hashicorp/vault/api"
    "healthcheck/core"
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