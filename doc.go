// Package wip provides working-tree checkpoints stored as native Git commits.
//
// Open targets an explicit repository directory. Capture uses a private index
// and conditional refs/wip/<branch> updates, preserving HEAD, normal branch refs,
// the real index, and working files. Tracked and non-ignored untracked files are
// captured by default; TrackedOnly excludes untracked files, and IncludeIgnored
// explicitly includes ignored files. Those options conflict.
// History, Compare, and ReadFile expose review data; Delete and Cleanup manage
// owned refs with observed-tip checks.
//
// All operations accept context and return structured results and errors. The
// library invokes local Git without prompts, terminal output, process-wide
// directory changes, or a dependency on the CLI. Git 2.30 or newer and Linux or
// macOS are required. Capture needs an attached local branch with an existing
// commit and does not support sparse worktrees, unmerged indexes, or gitlinks.
//
// Checkpoints are local and ephemeral: normal Git reflog expiry and garbage
// collection apply. They are not a backup system.
package wip
