---
title: Put human checkpoint messages before storage metadata
status: done
updated: '2026-10-10'
---

# Outcome

Ordinary Git logs and review tools display a meaningful subject such as an agent turn
label, while checkpoint ownership metadata remains strict and machine-readable.

## Scope and acceptance

- [x] Design a versioned commit-message layout with the human subject first and
  ownership metadata in an unambiguous footer or equivalent later message block.
  Keep branch/base validation and single-parent ownership rules intact.
- [x] Define framing and parsing so caller messages containing metadata-like text
  cannot be confused with the actual envelope. Preserve caller-message round trips,
  including multiline messages and trailing newlines, through `Checkpoint.Message`.
- [x] State how existing version-1 checkpoints are handled: explicit supported reading,
  migration, or a documented rejection/cutover policy. Do not silently reinterpret,
  overwrite, or delete existing refs. Legacy-tool history is a separate cutover concern.
- [x] Update capture, ownership parsing, history and inspection together; API results
  continue to omit internal metadata from user messages. Ref names and checkpoint
  ancestry do not change as part of this task.
- [x] Verify `git log --format=%s` shows the supplied human subject and a useful default
  for ordinary captures/baselines. Test message round trips, malformed/unknown metadata,
  branch/base mismatches, and the chosen previous-format policy with actual Git objects.
- [x] Update the storage protocol, requirements, library/CLI documentation, and any
  affected examples. Explain that session identity belongs in caller records rather
  than being inferred from the fixed Git author identity.

## Settled protocol

Write version 2 and read versions 1 and 2, including mixed-version parent chains.
Keep existing branch/base, direct-commit, and single-parent validation unchanged.
Version 1 retains its existing message decoding (remove the writer's one final
newline). No migration or rewriting existing objects is required.

Version 2 stores these exact bytes:

```text
DISPLAY_MESSAGE + "\n\ngo-git-wip:2 " + METADATA_JSON + "\n"
```

The metadata object has exactly `branch`, `base`, and `message_bytes` fields.
`message_bytes` is the nonnegative integer byte length of the original caller
message after applying the existing empty-message default (`Checkpoint` or
`Baseline`). Reject unknown, duplicate, missing, or incorrectly typed fields;
JSON field order and insignificant JSON whitespace are not ownership requirements.
Keep the metadata on one line, followed by exactly one final newline. NUL remains
an invalid caller-message byte.

Reserve raw messages beginning `go-git-wip:` for version-1/unknown-version dispatch.
For version 2, `DISPLAY_MESSAGE` normally equals the original message. If the
original begins `go-git-wip:`, prepend the visible string `Checkpoint: ` to the
display only. This narrow exception prevents ambiguity with a version-1 envelope;
ordinary human subjects remain unchanged, and the API returns the original bytes.

Decode in this order:

1. A raw body beginning `go-git-wip:` must satisfy the strict version-1 format.
   Unknown or malformed versions fail without trying the version-2 footer. A
   version-1 caller message may contain any footer-looking text without changing
   its interpretation.
2. Otherwise, read the final footer line only. Require version 2, strict metadata,
   the exact blank-line separator, and the final newline. Reject unknown or
   malformed footer versions without falling back to a different format.
3. Check the byte count against the display length before slicing. The display
   either consists of exactly that many original-message bytes, with no reserved
   leading namespace, or has the exact `Checkpoint: ` prefix followed by that
   many original-message bytes beginning `go-git-wip:`. Reject every other framing.
   Do not trim caller whitespace or trailing newlines.

Required regressions include a version-1 caller message ending in a complete
valid-looking version-2 footer, a version-2 message beginning with a complete
version-1-looking envelope, Unicode byte counts, multiline/trailing-newline
round trips, embedded footer text, bad lengths/framing, and unknown versions.
Exercise raw Git subjects, API history/inspection, and mixed-version chains.
This remains accidental-collision detection, not an authenticity boundary.

## Verification

Passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0:

- `make check`: formatting, vet, real Git API/CLI tests, external consumer vet/run.
- `make build`: CLI executable builds.
- `make race`: API/CLI tests pass with the race detector.
- `git diff --check`: no whitespace errors.

Real Git tests assert ordinary/default/baseline subjects and exact raw v2 bodies,
Unicode byte counts, multiline/whitespace/trailing-newline message round trips,
embedded footer text, and escaped legacy-looking leading caller messages. A v1
caller message ending in a complete valid-looking v2 envelope still decodes as v1.
Mixed stacks in both version orders support capture continuation, history,
inspection, and conditional deletion; unchanged captures do not rewrite legacy tips.
Strict malformed/unknown metadata, bad numeric types/lengths/framing, branch/base
mismatches, and NUL objects fail without overwrite/deletion or index/HEAD changes.
Malformed-object fixtures use Git's `hash-object --literally` in disposable repositories.
JSON field order/whitespace and valid numeric zero remain accepted. CLI tests verify
raw Git subjects, default labels, exact JSON history messages, and human first-line
history. The external consumer checks readable Git subject and API message equality.
Independent contract/code review approved without findings.
macOS and Git 2.30 have not been execution-tested.

## Resume

Complete. Implemented, reviewed, and verified. New captures write the settled v2
footer; readers retain v1 support and allow mixed-version stacks. Message-envelope
helpers are isolated in [checkpoint_message.go](../../../checkpoint_message.go);
commit identity/branch/base/ancestry/ref policies are unchanged. Caller messages
roundtrip exactly; only raw display of a leading reserved namespace receives the
canonical visible prefix described above. No migration is needed.
