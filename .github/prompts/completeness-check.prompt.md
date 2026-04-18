---
description: "caddy-fs-gcs: Check if staged changes have corresponding docs, tests, Caddyfile examples, and coverage."
agent: "agent"
argument-hint: "Optional: specific area to check"
---
Review staged changes and check if supporting artifacts exist:

1. Run `git diff --cached --stat` to identify staged changes
2. If nothing is staged, fall back to `git log origin/main..HEAD --stat` to check pushed commits on the branch
3. For each feature or behavioral change, verify:
   - Unit tests in matching `*_test.go` files
   - Doc updates in `docs/CONFIGURATION.md` or `docs/OPERATIONS.md`
   - Caddyfile examples in `Caddyfile.dev` or `e2e/Caddyfile.e2e`
   - Coverage meets 85% threshold (`task coverage:check`)
4. Report present vs missing as a checklist
5. Do not create anything, just report the gaps
