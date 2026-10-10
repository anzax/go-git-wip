---
title: Capture native checkpoints without changing workspace state
status: done
updated: '2026-10-10'
---

## Outcome and acceptance

Private index, capture policies, baseline/no-change, owned metadata, rollover, reflogs, conditional publication, cancellation and preservation tests.
Follow [requirements](../../../docs/requirements.md) and [architecture](../../../docs/architecture.md).

## Verification

Real Git tests cover baselines/no-change, fixed identity, tracked/staged eligibility,
partially staged index preservation, untracked/ignored policy, literal selections,
subdirectory paths, independent partial snapshots, branch rollover/reflogs, unsupported
states, invalid refs, SHA-256 objects, cancellation cleanup, and coordinated competing
capture/normal-branch writes. Astra reviewed contracts/correctness; message roundtrip
and partial file/directory collision findings were resolved without adding abstractions.
`make check` and `make build` pass on Linux/arm64 with Go 1.27.2 and Git 2.56.0.

## Resume

Capture complete. Next: commit reviewed [inspection](../20261010-1810-inspection/task.md),
then [CLI and embedding](../20261010-1820-cli-embedding/task.md).
