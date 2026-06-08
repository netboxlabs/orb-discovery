## Description

<!-- What does this PR change, and why? -->

## PR title checklist

This repo enforces a Conventional Commits PR title with **exactly one scope**
(`type(scope): subject`). The title becomes the squashed commit message and drives
the per-backend release. See [`CLAUDE.md`](../CLAUDE.md) for the full convention.

- [ ] Title is `type(scope): subject` with one scope from the allowlist:
      `device-discovery`, `gnmi-discovery`, `network-discovery`,
      `snmp-discovery`, `worker`, `ci`, `docs`, `deps`, `repo`.
- [ ] Type reflects intent — `feat`/`fix`/`perf` release the scoped backend;
      other types do not.
- [ ] Changes are scoped to the backend named in the title (or use `repo`).
