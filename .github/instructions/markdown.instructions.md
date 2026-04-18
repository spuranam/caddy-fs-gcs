---
description: "Markdown formatting rules for caddy-fs-gcs. Line length, fenced code blocks, tables, headings, and ASCII-only characters. Use when writing or editing markdown."
applyTo: "**/*.md"
---

# Markdown Authoring Rules

## Line Length (MD013)

- Maximum 120 characters per line
- Code blocks, tables, and headings are exempt
- Wrap long lines at natural sentence or clause boundaries

## Code Blocks (MD040)

- Every fenced code block **must** have a language identifier
- Use `bash` for shell commands, `go` for Go code, `text` for
  generic output, `caddyfile` for Caddyfile snippets
- When a code block contains backticks (Go raw strings, heredocs,
  template literals), use tilde fences instead of backtick fences

## Headings (MD022)

- Always leave a blank line before and after headings
- Do not place a list or code fence immediately after a heading

## Lists (MD032)

- Always leave a blank line before and after lists
- Do not start a list immediately after a heading or code fence

## Tables (MD060)

- Use consistent spacing around pipes: `| col1 | col2 |`
- Separator rows must match header style: `| ---- | ---- |`
- Always leave a blank line before and after tables

## Characters

Use only ASCII characters in markdown files:

- Use `--` instead of em dashes
- Use `---` for horizontal rules
- Use straight quotes (`"`, `'`) instead of curly/smart quotes
- Use `...` instead of ellipsis characters
- Use standard hyphens (`-`) instead of en dashes

## Linting

Config: `.markdownlint-cli2.jsonc` and `.markdownlint.json`. Run:

```bash
npx markdownlint-cli2 "**/*.md"
```

Auto-fix where possible:

```bash
npx markdownlint-cli2 "**/*.md" --fix
```
