# Task workflow

Directory-based tasks with no scripts, external tracker, or extra runtime.

Use `tasks/<bucket>/YYYYMMDD-HHMM-slug/task.md`, with creation time in Europe/Warsaw.
Buckets are `active`, `backlog`, and `archived`. Only task.md is mandatory.
Frontmatter holds `title`, `status`, and `updated`; status is `ready`, `active`,
`blocked`, `review`, or `done`. Bucket and status are separate: archiving does not
imply completion. Keep IDs unique and stable when moving tasks; repair incoming links.

1. Read the current task and inspect `git status --short` before work.
2. Record outcome, scope, observable acceptance criteria, and validation commands.
3. Keep the resume point current. Add log.md only for material evidence or decisions
   that do not fit the current task. Avoid transcripts and routine command logs.
4. Record checks actually run and limitations. Mark done only when criteria are met;
   use review when acceptance is pending. Put remaining work in a linked task.
5. Move completed task directories to `archived` and repair incoming links.

Keep artifacts with their task unless they are maintained project documentation.
Use relative links; root is `../../../` from a task folder. No automatic checkpoints,
normal commits, hooks, or publication are part of this simplified workflow.

Copy [_template.md](_template.md) to start a task. Find work with:

```sh
rg --files tasks
rg '^status:' tasks
```
