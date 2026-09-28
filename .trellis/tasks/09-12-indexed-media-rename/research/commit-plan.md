# Approved CI handoff

The user approved this complete proposal with `授权` on 2026-09-28. Authority
covers the listed source/task commit, branch push to origin, draft PR against
main, and CI follow-up including fixes through that PR. The inherited reviewed
changes are included. Merge, hosted rename, release, installation, and deployment
remain outside this approval. Execution evidence will be recorded separately.

## Source commit

- Repository: `D:/Repositories/rulego-indexed-vod`.
- Proposed branch: `feat/indexed-media-rename`, based on current `main`
  (`ab2217bccb46b8ca854b778ef82cd805e6941cb9`).
- Proposed commit: `feat!: rename indexed VOD to indexed media`.
- One coherent commit includes production naming, shipped consumers, checks,
  contracts, and this task documentation. It does not archive the task.

Files:

```text
.github/workflows/ci.yml
.trellis/spec/backend/quality-guidelines.md
README.md
examples/youtube-hls/chain.json
go.mod
node.go
node_test.go
plugin.go
producer.go
source_test.go
testdata/smoke-borrower-chain.json
testdata/smoke-node-pool.json
tests/e2e-hls-seek.sh
tests/e2e/node-pool.json
.trellis/tasks/09-12-indexed-media-rename/ (all task artifacts and research)
```

The continuation began with all listed tracked edits except `node_test.go`
already present, plus the untracked task directory. Main and the implementation
worker have extended `.github/workflows/ci.yml`, `source_test.go`, and task
records, and updated `node_test.go`. The inherited changes are reviewed as part
of this task; explicit approval must cover including them, not just new edits.

## CI transport

After explicit approval of commit and remote transport together:

1. Create the source branch and commit only the listed source/task paths.
2. Push that branch to the configured origin,
   `https://github.com/killbus/rulego-indexed-vod`.
3. Open a draft PR against `main`, titled
   `Rename indexed VOD to indexed media`. The existing `pull_request` trigger
   runs CI; pushing this feature branch alone does not trigger its `push` filter.
4. Inspect the candidate revision results for the Go test job, both architecture
   build/smoke jobs, and paired HLS seek job. Fix failures through the same PR.

Proposed PR description:

> Rename the module and sole RuleGo component to `rulego-indexed-media` /
> `indexedMedia`, updating shipped owner references, cache namespaces and build
> artifacts together. Old public types are rejected; paired H.264/AAC input,
> revision identity, storage paths and dependency/ABI pins remain unchanged.
>
> Local source/script, formatting and YAML/Bash checks are recorded in the task
> evidence. Compiled Go, ABI/shared-owner and paired HLS seek acceptance depends
> on this PR CI. Hosted rename, release/versioning and deployment are separate.

No merge, tag, repository rename, release, installation, or deployment is included
in this proposal.

## Execution result

- Approved source commit: `d388152eb2feea64a04b32f413fa750c42425289`.
- Branch pushed; draft PR: https://github.com/killbus/rulego-indexed-vod/pull/1.
- Owning CI run 36389841942 passed; A2-A5 evidence is now recorded in
  [acceptance-evidence.md](acceptance-evidence.md). A task-documentation follow-up
  records the result without changing the implementation.

## Other working trees

The engineering checkout has a pre-existing one-line capability-wording edit
and unrelated indexed-audio research. Neither belongs in the source commit or
this PR. Their existing state is preserved; the live authority route retains
the current repository name pending a separately authorized hosted rename.
