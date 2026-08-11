---
name: go-readability
description: >-
  Write and review Go code to meet Google readability standards. Covers style,
  naming, error handling, testing, documentation, package design, linting,
  mentor feedback, and CLI checklist. Use for "readability", "go style",
  "go naming", "go errors", "go testing", "idiomatic Go", "go best practices",
  "go code review", "go lint", or "go linting".
---

# Go Readability

Concise guide to writing Go that passes Google readability review. Distilled
from go/go-style/guide, go/go-style/decisions, and go/go-style/best-practices.

## Formatting

-   All code **must** be formatted with `gofmt` — use `hg fix`
-   No strict line length, but refactor long lines for clarity
-   Use `glaze` (not `build_cleaner`) to update BUILD files for Go
-   In Google3, **import paths are the source of truth** and BUILD files are
    derived from them using `glaze`. Do not manually edit Go BUILD files or try
    to influence import paths — Blaze determines them. Note that import paths
    often duplicate the last path segment (e.g., `"google3/net/goa/goa"`).
-   See go/go/layout for canonical Go package layout in Google3

## Linting

The **`go_lint` skill** is the canonical reference for lint commands and common
lint-rule names (`go/gocomments`, `go/gofmt`, `go/unusedresult`, `go/structtag`,
`go/interface`). Run `hg lint` for fast feedback or `tricorder analyze
-categories Lint,GoChecks -fix` for the full pass.

A few items worth knowing here too:

-   `hg fix` auto-formats Go files via `gofmt`
-   **For Go BUILD files use `glaze`, not `build_cleaner`.** `glaze
    //path/to:package` derives the BUILD targets from the `import` statements in
    your `.go` sources. Add `# keep` comments to preserve intentional deps:

    ```python
    go_library(
        deps = [
            "//path/to:dep",  # keep
        ],
    )
    ```

-   To suppress specific lint warnings, use `//nolint:RULE` comments:

    ```go
    value := someFunc() //nolint:errcheck
    ```

## Naming

Go uses **MixedCaps** (camelCase), not snake_case. **The only exception is test
function names**, where underscores separate the function under test from the
condition being tested (e.g. `TestParse_EmptyInput`, `TestParse_InvalidUTF8`).

Kind       | Convention                   | Example
---------- | ---------------------------- | -------------------------
Exported   | `UpperCamelCase`             | `UserManager`
Unexported | `lowerCamelCase`             | `userCount`
Package    | `lowercase` (no underscores) | `userutils`
Acronym    | All caps if exported         | `HTTPClient`, `xmlParser`

-   **Avoid** repeating the package name: `net.Conn` not `net.NetConn`
    (exception: eponymous types like `regexp.Regexp`)
-   **Avoid `Get` / `get` prefix** for plain getters: `u.Name()` not
    `u.GetName()`. If the call performs a remote/expensive operation that may
    take time, block, or fail, name it with a verb that signals that — use
    `Fetch` (for remote calls / I/O) or `Compute` (for expensive computation),
    NOT `Get`. See [`decisions#getters`](http://go/go-style/decisions#getters).
    -   `getRolloutKindUnitKind(...)` (calls gcloud) →
        `fetchRolloutKindUnitKind(...)`
    -   `GetUserCount()` (returns a stored field) → `UserCount()`
    -   `ComputeFingerprint()` (CPU-heavy hashing) → keep `Compute` prefix
-   **Receiver names**: 1-2 letters, abbreviation of the type, **applied
    consistently to every receiver for that type**. NEVER use `this`, `self`, or
    the full type name. `func (t *Tray) ...` not `func (tray *Tray) ...`, `func
    (this *X) ...`, or `func (self *X) ...`. See
    [`decisions#receiver-names`](http://go/go-style/decisions#receiver-names).
-   Avoid uninformative package names: `util`, `common`, `helper`, `model`
    (these often indicate poor package design). Domain-specific utility packages
    are fine (e.g., `rshellutil` for remote shell utilities).
-   Functions returning something → noun-like names, like `strings.Fields`
-   Functions doing something → verb-like names, like `strings.TrimSpace`

## Imports

-   Group: stdlib, then blank line, then google3 packages
-   Import paths for google3 start with `google3/`
-   Use `goimports` to manage import ordering
-   Renamed imports should be consistent across files
-   **Rename proto imports** to use a `pb` (for proto) or `grpc` (for gRPC)
    suffix (e.g., `foopb`). This is required for generated packages that contain
    underscores. See `references/google3_go_patterns.md` for details.

## Error Handling

-   **Always** handle errors explicitly — `if err != nil { ... }`
-   Return errors, don't panic (panic is for truly unrecoverable situations).
    See **Don't panic / Must functions** below.
-   Add context when wrapping: `fmt.Errorf("loading config: %w", err)`
-   Use `%w` to wrap errors **if callers need** `errors.Is`/`errors.As` (e.g.,
    to check for a specific sentinel error or extract a specific error type).
-   Use `%v` when you intentionally want to break the error chain or the caller
    does not need to inspect the underlying error (e.g., for security reasons,
    or when the underlying error is not meaningful to the caller).
-   Be cautious wrapping status errors across service boundaries — a wrapped
    `Unauthenticated` from a downstream call can mislead callers
-   See go/how-to-err-in-go for comprehensive error handling guidance
-   See go/go-style/best-practices#error-extra-info and
    https://go.dev/blog/go1.13-errors#whether-to-wrap for detailed guidance
-   Define sentinel errors as package-level vars: `var ErrNotFound =
    errors.New("not found")`
-   Use `google3/base/go/log` for logging, not the standard `log` package

```go
func ProcessRequest() {
    // ✗ Bad -- ignores error
    result, _ := DoSomething()

    // ✓ Good -- handles error
    result, err := DoSomething()
    if err != nil {
        return fmt.Errorf("processing request: %w", err)
    }
}
```

### Error string formatting

Per [`decisions#error-strings`](http://go/go-style/decisions#error-strings):

-   **Lowercase** (errors are usually wrapped in larger context before
    printing).
-   **No terminal punctuation** (no trailing `.`, `!`, or `?`).
-   **Exception**: starting with a proper noun, an acronym, or an exported Go
    identifier (`ProductConfig`, `RolloutKind`, `URL`, `IPv6`) is fine and
    expected — those keep their canonical capitalization.

```go
// ✗ Bad
err := fmt.Errorf("Something bad happened.")

// ✓ Good
err := fmt.Errorf("something bad happened")

// ✓ Good (exported identifier)
err := fmt.Errorf("ProductConfig has both UnitKindName and UnitKindNames set")
```

### Use `%q` for quoted strings

Per [`decisions#use-percent-q`](http://go/go-style/decisions#use-percent-q):
prefer `%q` over **manually wrapping** `%s` in single or double quotes. `%q`
escapes control characters and makes empty strings (`""`) visible — both
critical for debuggability. This applies to error strings, log messages, AND
test failure messages.

```go
// ✗ Bad: manual quotes
fmt.Errorf("failed to read RolloutKind '%s': %w", name, err)
fmt.Errorf("value \"%s\" looks like English text", text)
logger.Info("Checking RolloutKind '%s' at location '%s'...", name, loc)

// ✓ Good
fmt.Errorf("failed to read RolloutKind %q: %w", name, err)
fmt.Errorf("value %q looks like English text", text)
logger.Info("Checking RolloutKind %q at location %q...", name, loc)
```

**When NOT to use `%q`**: numeric values (`%d`, `%v`), error values (`%v` /
`%w`), slices/maps/structs (`%v` / `%+v`), and pre-formatted multi-line output
like `cmp.Diff` results (`%s`).

### Indent error flow (line of sight)

Per
[`decisions#indent-error-flow`](http://go/go-style/decisions#indent-error-flow):
handle errors first and return early. The "happy path" stays at the left-most
indent — never inside an `else` block. See also Go Tip #1:
[Line of Sight](http://go/gotip/episodes/1).

```go
// ✗ Bad: happy path is indented inside else
if err != nil {
    return err
} else {
    // normal code that looks abnormal due to indentation
    process(x)
    return nil
}

// ✓ Good: happy path stays at the left margin
if err != nil {
    return err
}
process(x)
return nil
```

### Don't panic / `Must` functions

Per [`decisions#dont-panic`](http://go/go-style/decisions#dont-panic) and
[`decisions#must-functions`](http://go/go-style/decisions#must-functions):

-   **Don't `panic`** for normal error handling. Return an `error` and multiple
    return values.
-   In `package main` / initialization code, prefer `log.Exit` over `panic` for
    errors that should terminate the program (no stack trace needed for
    user-facing config errors).
-   For **package-level variable initializers** that genuinely cannot fail after
    a one-time setup, the `MustXYZ` (or `mustXYZ`) naming convention signals
    "panics on failure". Use sparingly:
    -   `template.Must`, `regexp.MustCompile`, `MustParse(...)`.
    -   In tests, `must*` helpers are fine if they call `t.Fatal` (mark with
        `t.Helper()`).
    -   NEVER call a `Must` function on user input or in a request handler —
        only on package-init constants.

## Documentation

-   **All exported names** must have doc comments
-   Comments are **full sentences**, ending with a period, starting with the
    name being documented (e.g., `// Foo does X.`).
-   Package comment: `// Package foo provides ...`
-   Function comment: `// FetchUser retrieves a user by ID.`
-   Use `//` comments, not `/* */`

```go
// A UserManager manages the lifecycle of user accounts.
type UserManager struct { ... }

// Create creates a new user with the given name.
func (m *UserManager) Create(name string) (*User, error) { ... }
```

## Testing

-   Use the standard `testing` package
-   **Table-driven tests** are strongly preferred
-   Test function names: `TestFunctionName` for single-scenario tests, or
    `TestFunctionName_Condition` when one target has multiple test functions
    (e.g. `TestParse_EmptyInput`, `TestParse_InvalidUTF8`). Underscores are
    permitted in test, benchmark, and example names ONLY (see
    [decisions#mixed-caps](http://go/go-style/decisions#mixed-caps)). When
    showing example test code, prefer the underscore form to model the
    convention.
-   Use `t.Helper()` in test helper functions
-   Use `cmp.Diff` (from `google3/third_party/golang/cmp/cmp`) for deep
    comparisons
-   Assert libraries (and custom assert helpers) are discouraged — use standard
    `t.Errorf`/`t.Fatalf` (see go/go-style/decisions#assert).
-   See go/go-test-examples for patterns
-   To get the context for use in tests, use `ctx := t.Context()` from `t
    *testing.T` instead of `ctx := context.Background()`. `t.Context()` (Go
    1.24+) is automatically canceled when the test ends, preventing goroutine
    leaks and honoring test timeouts.

```go
func TestAdd_TableDriven(t *testing.T) {
    tests := []struct {
        name string
        a, b int
        want int
    }{
        {name: "positive", a: 1, b: 2, want: 3},
        {name: "zero", a: 0, b: 0, want: 0},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            if got := Add(tc.a, tc.b); got != tc.want {
                t.Errorf("Add(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
            }
        })
    }
}
```

### Useful test failures (CRITICAL)

A test that fails should be diagnosable WITHOUT reading the test source. Use the
canonical failure-message format:

```text
YourFunc(<inputs>) = <got>, want <want>
```

Per
[`decisions#useful-test-failures`](http://go/go-style/decisions#useful-test-failures),
the message must convey: **what caused the failure**, **the inputs**, **what was
actually returned**, **what was expected**.

The two most-cited mentor comments here:

1.  **Identify the function**
    ([decisions#identify-the-function](http://go/go-style/decisions#identify-the-function))
    in the failure message, even if `TestXxx` makes it obvious.

2.  **Identify the input**
    ([decisions#identify-the-input](http://go/go-style/decisions#identify-the-input)):
    print the function inputs in `%v` form. Under a subtest (`t.Run`), the
    subtest name already prefixes the failure, so there's no need to repeat the
    case `name` (or `desc`, if you have one) in the message itself.

```go
// ✗ Bad: missing both function name and inputs
t.Errorf("got %q, want %q", got, want)

// ✗ Bad: function name present but inputs missing
t.Errorf("rolloutKindUnitKind() = %q, want %q", got, want)
t.Errorf("groupRolloutKindsByUnitKind() succeeded, expected error")

// ✓ Good: function + inputs + got + want, in the canonical form.
t.Errorf("rolloutKindUnitKind(%+v, %v) = %q, want %q",
    rk, unitKindNames, got, want)
t.Errorf("groupRolloutKindsByUnitKind(%q, %v, %+v) succeeded, want error",
    env, unitKindNames, rolloutKinds)
```

Other rules in this family (got-before-want, keep-going, subtest-names,
compare-full-structures, test-error-semantics, print-diffs, level-of-detail)
plus full examples are in
[`references/testing_failures.md`](references/testing_failures.md). **Read that
file before writing new `t.Errorf` / `t.Fatalf` messages.**

Quick rules to keep in mind:

-   **Got before want** for `cmp.Diff(want, got)`: include the legend `(-want
    +got)` in the message.
-   **`t.Error` over `t.Fatal`** unless subsequent checks would be meaningless.
-   **Subtest names**: identifier-style. Underscores are fine
    ([go/gotip/117](http://go/gotip/episodes/117)); avoid slashes — they collide
    with `--test_filter`.
-   **Use `%q`** for string inputs/outputs in failure messages so empty strings
    and control chars are visible.

## Interfaces

-   Keep interfaces **small** — prefer 1-2 methods
-   Define interfaces at the **consumer**, not the producer
-   Name single-method interfaces with `-er` suffix: `Reader`, `Writer`
-   Accept interfaces, return concrete types

## Concurrency

-   Use goroutines and channels **sparingly**, only where needed
-   Always use `context.Context` for cancellation and deadlines
-   Pass `ctx` as the **first parameter**
-   Don't store `context.Context` in structs

## Package Design

-   A package should be the transitive closure of closely related ideas — all
    types and functions a caller would naturally use together
-   Avoid circular dependencies between packages
-   Avoid type aliases and `internal` packages as workarounds for package
    structure; restructure instead
-   Use `google3/base/go/` for Google-specific startup, flags, logging
-   Every Go program's `main` function should call `google.Init()` instead of
    `flag.Parse()`

## Receiver type (pointer vs value)

Per [`decisions#receiver-type`](http://go/go-style/decisions#receiver-type):
**correctness wins over speed or simplicity**. Quick rules:

-   **MUST** use a pointer receiver if the method mutates the receiver, or if
    the struct contains fields that cannot safely be copied (e.g. anything
    embedding `sync.Mutex`).
-   **Use a value receiver** for small types whose methods don't mutate state.
-   **Make the methods for a type either all-pointer or all-value** — don't mix.
-   When in doubt: pointer receiver.

## Switch & break

Per [`decisions#switch-break`](http://go/go-style/decisions#switch-break): Go
`switch` cases automatically break — a bare `break` is redundant. To break out
of an enclosing `for`, use a labeled `break`:

```go
loop:
for {
    switch x {
    case "A":
        break loop  // exits the loop
    }
}
```

## Nil slices

Per [`decisions#nil-slices`](http://go/go-style/decisions#nil-slices): prefer
`var s []T` over `s := []T{}` for empty-slice declarations. `len`, `cap`,
`range`, and `append` all work on nil slices. Don't design APIs that force
callers to distinguish nil from empty — use `len(s) == 0` to test for emptiness,
not `s == nil`.

```go
// ✓ Good
var t []string

// ✗ Bad (when nothing forces non-nil)
t := []string{}
```

## Common Gotchas

-   Don't shadow named returns — it causes subtle bugs
-   `defer` runs at function exit, not scope exit
-   Slices share underlying arrays — copy if needed: `slices.Clone(s)`
-   `nil` maps can be read but panic on write — always `make(map[K]V)`
-   String iteration yields runes, not bytes — `len(s)` returns byte count, not
    character count. Use `utf8.RuneCountInString(s)` for rune count or
    `uniseg.StringWidth(s)` for display width

## Deep-Dive References

For more detail on specific topics, see these reference files:

-   `references/common_mentor_feedback.md` — Detailed guide to the most frequent
    readability mentor comments (naming, error handling, documentation,
    interfaces, concurrency, common gotchas)
-   `references/testing_failures.md` — Authoritative deep-dive on the 9
    sub-decisions under `decisions#useful-test-failures`. Read before writing
    any new `t.Errorf` / `t.Fatalf` message.
-   `references/decisions_index.md` — One-line-per-rule index of every
    `go/go-style/decisions` section, mapping each to "covered" / "covered
    partial" / "not covered" in this skill plus the relevant SKILL.md section.
    Use to find authoritative guidance fast.
-   `references/google3_go_patterns.md` — Quick reference for Google3-specific
    Go patterns (imports, logging, flags, protos, gRPC, testing with cmp.Diff)
-   `references/self_audit_checklist.md` — Mechanical greps for catching the
    most common style violations before sending a CL.
-   `references/cli_review_checklist.md` — Comprehensive Go CLI review checklist
    (~45 patterns across 12 categories) derived from real reviewer feedback on 8
    production Google Workspace Go CLs
-   `references/cobra_cli_patterns.md` — Cobra-specific patterns (package doc
    scope, `ctx` naming, testing `package main`) derived from code review
-   `examples/before_after_examples.md` — Before/after code transformations
    showing common fixes mentors request

## Review Guidelines

When reviewing or generating Go code, check for these common pitfalls:

-   Find fragments of duplicated code that could be refactored into helper
    functions
-   Spot places where errors are being dropped
-   Spot potential injection vulnerabilities
-   Spot potentially dangerous operations that may cause data loss

For Go CLI code specifically, see `references/cli_review_checklist.md` for a
detailed checklist covering 12 categories of issues found in production reviews:

1.  **Duplicate Code** — Consider extracting repeated logic into helpers when it
    improves clarity (a little duplication is sometimes preferable to premature
    abstraction)
2.  **Function Naming** — Names must describe what the function actually does
3.  **Command Design** — Split multi-mode commands into one-command-per-action
4.  **Conflicting Flag Guards** — Guard against mutually exclusive flags
5.  **Error Handling** — Never silently ignore errors
6.  **Input Sanitization** — Sanitize user input before embedding in structured
    formats
7.  **Security & Privacy** — Prevent unintentional data exposure
8.  **Dangerous Operations** — Require confirmation for destructive actions
9.  **Control Flow Clarity** — Restructure complex conditionals for readability
10. **Logic Bugs** — Watch for dead code, ignored output formats, missing
    `ForceSendFields`
11. **Idiomatic Go** — Prefer `strings.Cut`, `slices.Repeat`, descriptive names
12. **Code Style** — Consolidate var declarations, match CLI names in help text

## Key Resources

-   go/go-style/guide — Core style guide
-   go/go-style/decisions — Style decisions
-   go/go-style/best-practices — Best practices
-   go/gotip/episodes — Go Tips of the Week
-   go/go-test-examples — Testing patterns
-   go/go-errors-crash-course — Error handling guide
-   go/golint — Golint documentation
-   go/glaze — Glaze BUILD file management
-   go/tricorder-cli — Tricorder documentation

## Self-audit checklist (run before sending a CL)

For mechanical greps that catch the most common style violations (use-percent-q,
error-strings, identify-the-function, identify-the-input, getters,
indent-error-flow, switch-break, use-any, nil-slices, receiver-names) see
[`references/self_audit_checklist.md`](references/self_audit_checklist.md).
False positives are expected; the greps are starting points for inspection.

## Reporting Issues

Report bugs or improvements for this skill at
[Agent Skill: go_readability](http://b/hotlists/8076906). See the `skill_issue`
skill for instructions on filing and triaging skill bugs.
