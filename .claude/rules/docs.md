---
paths:
  - "**/*"
---

# Documentation rules (always loaded)

This rule governs how subsystem documentation is authored and maintained in this repo. It always loads so the conventions apply regardless of which files are being edited.

## Where docs live

- **Default: path-scoped rule** at `.claude/rules/docs-<topic>.md`.
  - Each rule MUST declare a `paths:` frontmatter listing the globs that trigger it. Rules auto-load when Claude reads/edits a matching file — no manual invocation.
  - Scope `paths:` tightly to the subsystem's actual files (routes, libs, migrations, workflows). Do not use broad wildcards like `**/*` on topical rules — that defeats the auto-load gating and bloats context.
  - Name the file `docs-<topic>.md` (kebab-case topic). Keep one topic per file.
- **Promote to a skill** (`.claude/skills/docs-<topic>/SKILL.md`) ONLY when the content is too large to keep in context unconditionally — typically because it ships bundled `references/`, `scripts/`, or other heavy assets.
  - When promoting, also add a thin pointer rule `.claude/rules/docs-<topic>-trigger.md` with `paths:` frontmatter that tells Claude to invoke the skill on demand for matching files.
- **Do not** add long-form subsystem documentation to `README.md`, `CLAUDE.md`, or ad-hoc markdown files. `CLAUDE.md` should only carry the index of rules plus project-wide conventions.

## Frontmatter shape

Every `docs-*.md` rule starts with:

```
---
paths:
  - "path/glob/one/**"
  - "path/glob/two.ts"
---
```

After the frontmatter, open with a single `#` title and a one-paragraph summary of what the subsystem does and when this rule is useful. Then go deep.

## Thin pointer rule for skills

When a topic is promoted to a skill, the companion trigger rule stays tiny — just enough to fire on the right paths and tell Claude to invoke the skill. Shape:

```
---
paths:
  - "src/lib/<subsystem>.ts"
  - "src/app/api/<subsystem>/**"
---

# <Subsystem> — reference

When working in the matched files, invoke the **`docs-<topic>`** skill. It carries the full reference (bulky assets: `references/`, `scripts/`, long field tables, worked examples) which is too large to keep in context unconditionally — load it on demand.

Trigger it for any of: <list the concrete situations where the skill earns its keep — specific error codes, method names, flows, or decisions>.
```

Keep the trigger rule under ~20 lines. All the real content lives in the skill's `SKILL.md`.

## Keeping docs current

- **When you change code covered by a rule or skill, update the rule/skill in the same PR.** Stale docs are worse than no docs.
- **When you add a new subsystem, add a `docs-<topic>.md` rule for it** (or a skill if the content is heavy) in the same PR, and register it in the index in `CLAUDE.md`.
- **After creating or updating any rule or skill, run the `docs-reviewer` agent** (`.claude/agents/docs-reviewer.md`) to cross-check the documentation against the actual codebase for factual accuracy.
- When a subsystem is deleted, delete its rule/skill and remove the entry from the `CLAUDE.md` index in the same PR.

## Index in CLAUDE.md

`CLAUDE.md` maintains a short index of every `docs-*.md` rule and every `docs-*` skill, with a one-line description of each. Update the index whenever you add, rename, or remove a rule/skill.
