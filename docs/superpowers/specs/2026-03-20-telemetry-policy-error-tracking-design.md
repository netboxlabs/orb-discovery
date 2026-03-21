# Design: Runtime Error Tracking for Telemetry Policy Backends

**Date**: 2026-03-20
**Scope**: snmp-telemetry, probe-telemetry, flow-telemetry
**Status**: Approved

---

## Problem

Telemetry backends currently hardcode `Status: "running"` for all active policies. When a runtime error occurs — SNMP authentication failures, connection timeouts, unexpected process crashes — the error is only written to logs. Operators have no way to discover that a policy is malfunctioning without tailing log files. The `GET /api/v1/policies` and `list_policies` MCP tool give a false picture of health.

## Goal

Surface the most recent runtime error for each policy via the existing `GET /api/v1/policies` endpoint. Errors self-heal: when the next successful run cycle completes, the error is cleared and status returns to `"running"`. No persistent storage is added; state is in-memory only.

---

## Data Model

### `policy.Status` struct (extended)

```go
type Status struct {
    Name        string     `json:"name"`
    Status      string     `json:"status"`                  // "running" | "running_with_errors"
    LastError   *string    `json:"last_error,omitempty"`    // nil when healthy
    LastErrorAt *time.Time `json:"last_error_at,omitempty"` // nil when healthy
}
```

### Status values

| Value | Meaning |
|-------|---------|
| `"running"` | Policy is active, last run succeeded (or no errors seen) |
| `"running_with_errors"` | Policy is active but the last run failed |

### Example API response

```json
[
  { "name": "ok-policy", "status": "running" },
  {
    "name": "bad-creds-policy",
    "status": "running_with_errors",
    "last_error": "SNMP walk failed: authentication failure",
    "last_error_at": "2026-03-20T10:15:00Z"
  }
]
```

---

## Architecture

### Runner error state (thread-safe)

Each telemetry `Runner` struct gains three fields and three methods:

```go
// Fields added to Runner struct
mu        sync.RWMutex
lastErr   error
lastErrAt time.Time

// SetError records a runtime failure
func (r *Runner) SetError(err error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.lastErr = err
    r.lastErrAt = time.Now()
}

// ClearError removes any recorded error (called on successful run).
// Note: sync.RWMutex is zero-value safe; no explicit initialization needed in NewRunner.
func (r *Runner) ClearError() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.lastErr = nil
    r.lastErrAt = time.Time{} // reset to avoid stale timestamp
}

// GetLastError returns the most recent error and when it occurred
func (r *Runner) GetLastError() (error, time.Time) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.lastErr, r.lastErrAt
}
```

### Manager status assembly

`GetPolicyStatuses()` reads error state from each runner:

```go
func (m *Manager) GetPolicyStatuses() []Status {
    statuses := make([]Status, 0, len(m.policies))
    for name, runner := range m.policies {
        s := Status{Name: name, Status: "running"}
        if err, at := runner.GetLastError(); err != nil {
            msg := err.Error()
            s.Status = "running_with_errors"
            s.LastError = &msg
            s.LastErrorAt = &at
        }
        statuses = append(statuses, s)
    }
    return statuses
}
```

---

## Error Detection per Backend

### snmp-telemetry

**Location**: `snmp-telemetry/policy/runner.go` — gocron `DurationJob` function

SNMP metric collection runs periodically per target. One gocron job is created per expanded IP, all sharing the same `*Runner`. A naive `SetError`/`ClearError` per job would create a race: a succeeding target's `ClearError` would mask a concurrently failing target's error.

**Approach: per-target error map**

The Runner tracks errors per target in a thread-safe map:

```go
// Additional field on Runner (protected by the existing r.mu, no separate mutex needed)
targetErrs   map[string]error   // key = "host:port"; initialized in NewRunner

func (r *Runner) SetTargetError(target string, err error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.targetErrs[target] = err
    r.lastErrAt = time.Now()
    r.lastErr = r.buildCombinedError() // must be called with r.mu held
}

func (r *Runner) ClearTargetError(target string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.targetErrs, target)
    if len(r.targetErrs) == 0 {
        r.lastErr = nil
        r.lastErrAt = time.Time{} // reset stale timestamp when all targets recover
    } else {
        r.lastErr = r.buildCombinedError() // must be called with r.mu held
    }
}
```

`buildCombinedError()` (private, must only be called with `r.mu` held) returns an error summarizing all failing targets, e.g. `"metrics collection failed: 192.168.1.1: timeout; 192.168.1.2: authentication failure"`.

The `SetError`/`ClearError`/`GetLastError` public API remains the same; the map is an internal implementation detail. `targetErrs` must be initialized to an empty map in `NewRunner`.

### probe-telemetry

**Location**: `probe-telemetry/policy/runner.go` — cloudprober goroutine in `Start()`

Cloudprober handles probe-level errors internally (exported as metrics). The only Go-level detectable failure is the prober goroutine exiting unexpectedly before context cancellation:

```go
go func() {
    r.prb.Start(r.ctx)
    // r.ctx.Err() is non-nil only when Stop() → cancel() was called (normal shutdown).
    // If the goroutine exits with ctx still active, the prober crashed unexpectedly.
    if r.ctx.Err() == nil {
        r.SetError(fmt.Errorf("prober exited unexpectedly"))
    }
}()
```

No `ClearError` is needed: an unexpected prober exit is unrecoverable without deleting and resubmitting the policy. The `r.ctx.Err() == nil` guard is essential — without it, normal `Stop()` → `cancel()` would incorrectly set an error.

### flow-telemetry

**Locations**: `flow-telemetry/flow/listener.go` (signature change) + `flow-telemetry/policy/runner.go` (consumption)

`flow.NewListener` currently returns `(<-chan FlowRecord, error)`. The `UDPReceiver` (`recv`) is a local variable inside `NewListener`; its `recv.Errors()` channel is drained by an internal goroutine that only logs errors — they are never surfaced to callers.

**Required change to `NewListener` signature (Option A)**:

```go
// Before:
func NewListener(ctx context.Context, ...) (<-chan FlowRecord, error)

// After:
func NewListener(ctx context.Context, ...) (<-chan FlowRecord, <-chan error, error)
```

The internal goroutine that currently logs `recv.Errors()` items now also forwards them to the returned error channel (buffered, e.g. capacity 16 to avoid blocking). The channel is closed when the goroutine exits (i.e. when `ctx.Done()` fires and `recv.Errors()` is drained).

**Runner detection** (in `Start()`):

```go
flowCh, errCh, err := flow.NewListener(r.ctx, ...)
if err != nil {
    return err
}
go func() {
    for rec := range flowCh {
        r.window.Add(rec)
    }
}()
go func() {
    for err := range errCh {
        r.SetError(fmt.Errorf("flow listener error: %w", err))
    }
    // errCh closed without context cancellation = listener stopped unexpectedly
    if r.ctx.Err() == nil {
        r.SetError(fmt.Errorf("flow listener stopped unexpectedly"))
    }
}()
```

The caller of `runner.Start()` in `policy/manager.go` already handles the `error` return and does not need to change.

---

## MCP Server Update

Update the `list_policies` tool docstring in `mcp-server/src/orb_mcp/tools/agent.py` to:

1. Correct existing inaccuracy: add `flow-telemetry` to the list of supported telemetry agent types (currently only snmp-telemetry and probe-telemetry are mentioned, but `_TELEMETRY_AGENTS` already includes flow-telemetry)
2. Document the new response fields:
   - `status`: `"running"` (healthy) or `"running_with_errors"` (policy active but last run failed)
   - `last_error` (optional string): human-readable error from the most recent failure
   - `last_error_at` (optional RFC3339 timestamp): when the error last occurred
3. Explain self-healing: errors clear automatically on the next successful run; `"running_with_errors"` does not mean the policy has stopped

---

## Files Changed

| File | Change |
|------|--------|
| `snmp-telemetry/policy/runner.go` | Add error fields + methods; call SetError/ClearError in job |
| `snmp-telemetry/policy/manager.go` | Extend Status struct; update GetPolicyStatuses() |
| `probe-telemetry/policy/runner.go` | Add error fields + methods; wrap prober goroutine |
| `probe-telemetry/policy/manager.go` | Extend Status struct; update GetPolicyStatuses() |
| `flow-telemetry/flow/listener.go` | Change `NewListener` signature to return `<-chan error` |
| `flow-telemetry/policy/runner.go` | Add error fields + methods; consume error channel from NewListener |
| `flow-telemetry/policy/manager.go` | Extend Status struct; update GetPolicyStatuses() |
| `mcp-server/src/orb_mcp/tools/agent.py` | Update `list_policies` docstring |

No changes to `server.go` files — new fields appear automatically in JSON responses.

---

## Testing

### Unit tests (per backend)

**Runner-level tests** (test the struct directly, bypassing `NewRunner` to avoid heavyweight dependencies):

```go
r := &Runner{} // or &Runner{targetErrs: map[string]error{}} for snmp-telemetry
// 1. Initial state: GetLastError returns nil
// 2. After SetError: GetLastError returns the error and a non-zero timestamp
// 3. After ClearError: GetLastError returns nil, time.Time{}
```

**Manager-level tests** (inject runner directly into `m.policies`):

1. Single policy, no error: `GetPolicyStatuses()` returns `status: "running"`, nil `LastError`/`LastErrorAt`
2. Single policy, error set: returns `status: "running_with_errors"`, non-nil `LastError` and `LastErrorAt`
3. Single policy, error then cleared: returns `status: "running"`, nil fields
4. Two policies, one healthy and one with error: results are independent (healthy policy is not affected by the other's error)

### Integration

Submit a policy with invalid SNMP credentials via `mcp__orb-discovery__submit_policy`, wait one metrics interval, then call `mcp__orb-discovery__list_policies` — expect `running_with_errors` with auth error message.

### Build & test

```bash
cd snmp-telemetry  && go build ./... && go test ./...
cd probe-telemetry && go build ./... && go test ./...
cd flow-telemetry  && go build ./... && go test ./...
```
