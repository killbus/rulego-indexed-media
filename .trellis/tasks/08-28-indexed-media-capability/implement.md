# Implementation plan

1. Replace the public node contract with `indexedVod` operations over a strict
   normalized video/audio media lease. Remove all YouTube, yt-dlp, selector,
   sort, cookie, provider-expiry, and manifest fields and code.
2. Rework the shared manager around bounded SIDX/index caching keyed by the
   complete normalized lease. Make every operation consume the current lease
   and return typed stale/revision failures instead of resolving.
3. Keep the exact indexed MP4 Range and one-segment mux implementation. Consume
   the origin-issued publication deadline and separate source retry behavior
   from FFmpeg invocation retry behavior.
4. Replace boundary tests while preserving SIDX, range, concurrency, revision,
   output-bound, path-safety, retry, shutdown, and shared-owner coverage.
5. Rewrite the example RuleGo flow to resolve YouTube through the existing
   yt-dlp wrapper, normalize the selected representations, compose HLS, and
   perform one bounded stale-lease refresh/retry outside `indexedVod`.
6. Validate direct HLS playlist response through RuleGo's existing
   `responseToBody` and `metadataToHeaders` processors; do not introduce a
   manifest writer.
7. Run lightweight local formatting, vet, unit and race checks. Use GitHub CI
   for ABI-pinned amd64/arm64 plugin builds and smoke loading.
8. Verify initial playback, distant/near-end seek, same-member concurrency,
   RuleGo restart, lease expiry/refresh, retained-byte bounds, and absence of
   complete upstream representations.
9. Commit the capability-boundary change as one coherent new-task commit,
   rename the repository, publish the breaking release, verify checksums/ABI/
   example artifacts, and archive the task with final-state evidence only.

## Rollback points

- Do not move resolver semantics back into `indexedVod` to simplify the example
  chain.
- Do not change `resourceOrigin`, yt-dlp-http-wrapper, or ffmpeg-over-ip
  protocol to make this implementation fit.
- Do not retain compatibility aliases or old request fields as a fallback.
