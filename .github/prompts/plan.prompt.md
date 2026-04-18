---
description: "caddy-fs-gcs: Create an implementation plan for a feature. Produces a structured blueprint with architecture decisions, task breakdown, interface design, and testing strategy."
agent: "planner"
argument-hint: "Describe the feature to plan (e.g., 'Add directory listing support')"
---
Create a structured implementation blueprint for the described feature:

1. **Summary** -- What and why
2. **Architecture decisions** -- Layers affected, new types, interface changes
3. **Task breakdown** -- Ordered steps with files, complexity, dependencies
4. **Interface design** -- Define contracts first
5. **Error handling** -- Sentinel errors, wrapping strategy
6. **Testing strategy** -- Unit tests, benchmarks, e2e tests (85%+ coverage)
7. **Documentation** -- Docs, Caddyfile examples
8. **Risks & edge cases** -- What could go wrong

Follow caddy-fs-gcs conventions: Caddy module interfaces, `caddy.UsagePool`, `zap.Logger`, fs.FS patterns.
