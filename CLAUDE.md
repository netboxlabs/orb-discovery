# orb-discovery — contributor & agent guide

`orb-discovery` is a multi-backend monorepo. Each backend is versioned and
released independently, and the release pipeline keys off **PR titles** — so
titles must follow the convention below.

## How releases work

- PRs are **squash-merged into `develop`**; the PR title becomes the single
  commit message. `develop` is later promoted to `release`, which triggers each
  backend's release workflow.
- A backend only releases from commits scoped to it; the version bump and the
  changelog are computed from that backend's commits alone (filtered by the
  files the commits touched under `<backend>/**`).
- A required check (**Validate PR title**) blocks merge into `develop` when the
  title doesn't match the convention.

## PR title convention (enforced)

Use a Conventional Commits title with **exactly one scope**:

```
<type>(<scope>): <subject>
```

### Allowed types

`feat`, `fix`, `perf`, `refactor`, `chore`, `docs`, `test`, `build`, `ci`, `revert`

### Allowed scopes (pick exactly one)

Releasable backends — `feat`/`fix`/`perf` here cuts a release for that backend:

- `device-discovery`
- `gnmi-discovery`
- `network-discovery`
- `snmp-discovery`
- `worker`

Non-release scopes — pass the check but never cut a release:

- `ci` — CI / workflow changes
- `docs` — documentation
- `deps` — dependency bumps (`chore(deps)` cuts a patch for the touched backend)
- `repo` — repo-wide / cross-cutting changes

### Type → release mapping

| Type | Release |
|------|---------|
| `fix` | patch |
| `feat` | minor |
| `perf` | patch |
| `chore(deps)` | patch |
| breaking (`!` suffix or `BREAKING CHANGE:` footer) | major |
| everything else | none |

### Keep PRs scoped

Prefer one backend per PR so the scope is unambiguous. Cross-cutting work should
use the `repo` scope or be split into per-backend PRs.

## Examples

- `feat(snmp-discovery): add VLAN membership translation`
- `fix(gnmi-discovery): guard against self-referential LAG`
- `perf(network-discovery): batch NAPALM getters`
- `chore(deps): bump go.opentelemetry.io/otel/sdk`
- `ci(repo): scope releases to per-backend commits`
