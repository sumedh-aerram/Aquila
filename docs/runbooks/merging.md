# Merging changes

Use a linear history and rebase-merge pull requests. This is the repository's contribution procedure; the public workflow does not configure or prove branch protection (`.github/workflows/ci.yml`). The maintainer performs staging, commits, pushes, and merges.

For stacked changes, open the lower layer first and set the upper pull request's base to the lower branch. Review the upper diff against that base so it shows only its own scope. After the lower layer lands, rebase the upper branch onto the updated target, resolve conflicts, rerun the affected checks, and update the pull request base. Preserve review context in the description if the diff changes materially.

Do not merge a documentation claim of `validated` from a CLI exit code alone. For experiment changes, attach the evidence JSON and report executed steps, blockers, revision identities, and whether the run used built or declared gateways (`internal/cli/plan.go:RunExperiment`, `internal/evidence/artifact.go:Artifact`). For CI changes, inspect the hosted job log and JSON evidence assertions in `.github/workflows/ci.yml`.

Before requesting merge, verify the working-tree diff, public-document links, required checks, and that no generated outputs or private inputs are included. [Contributing](../../CONTRIBUTING.md) covers commit format and local test commands. Avoid squashing together unrelated changes when they have separate review paths.
