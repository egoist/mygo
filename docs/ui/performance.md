# Checked element API performance

Measured on October 8, 2026 on Apple M5, macOS arm64, Go 1.27.1,
`CGO_ENABLED=0 GOMAXPROCS=1`. The original API baseline is `fab604b`
(MyGo 0.2.18). The earlier PR design is `4f9c3f6`, before the revision that
keeps `*Context` and uses direct element owner records.

Matching fixtures build 1,000 static labels, or visible rows of a million-row
list. Data, dimensions, styles and warm-up are the same. The list baseline
uses its original four-argument constructor; the checked API uses a keyed
fluent list. Frames render through `ui.Tester` on the CPU. Runs were sequential,
six samples per fixture with 300 ms per sample; figures are medians.

| Fixture | Original API | Earlier PR | Revised API |
|---|---:|---:|---:|
| 1,000 labels: time/frame | 400 µs | 429 µs | 397 µs |
| 1,000 labels: allocations/frame | 0 | 0 | 0 |
| 1,000 labels: allocated bytes/frame | 0 | 0 | 0 |
| Virtual list: time/frame | 14.4 µs | 15.4 µs | 14.5 µs |
| Virtual list: allocations/frame | 20 | 23 | 20 |
| Virtual list: allocated bytes/frame | 144 | 280 | 176 |
| Warmed 1,000-label view: retained Go heap | ~2.43 MB | ~2.43 MB | ~2.50 MB |

The revised times are within about 1% of the original in these samples;
this is not a claim of a speedup. Element validation uses a direct owner,
slot and generation. It no longer resolves a weak pointer on every call,
enters a parent scope for each factory, or tracks/replays deferred styles.
Static label handles do not allocate. Input bindings, action queues and
persistent identity bindings reuse storage once warmed. The fluent list's
callback captures more data than the original callback, adding 32 allocated
bytes per frame without adding allocations in this fixture. Composite
controls may allocate callback closures; the fixtures do not cover all widgets.

`BenchmarkDiffSteady` remains at **2 allocations / 72 bytes per frame**
(median 389 µs in the revision). This benchmark builds through the private
renderer API and verifies its allocation floor; the public fixtures above
measure checked handles.

`TestValueFrameRetainedHeap` warms fonts and rendering, releases a warm view,
forces collection, then measures a second retained view after ten frames
and another collection. Three samples gave about 2.50 MB for the revised API,
roughly 3% above the original. This is Go heap, including the headless host;
it does not measure native window process memory or GPU resources.

Earlier exploratory runs varied while compilation and other tests ran.
The table uses isolated runs. Recheck small timing differences on CI and
other machines; allocation counts are more stable. These are steady-frame
measurements, not benchmarks against another GUI framework.

## Reproduce

```sh
CGO_ENABLED=0 GOMAXPROCS=1 go test ./ui -run '^$' \
  -bench '^(BenchmarkValueFrame|BenchmarkValueList|BenchmarkDiffSteady)$' \
  -benchmem -count=6 -benchtime=300ms
CGO_ENABLED=0 GOMAXPROCS=1 go test ./ui \
  -run '^TestValueFrameRetainedHeap$' -count=3 -v
```

For the original API, export `fab604b` to a separate directory and copy
`ui/testdata/legacy_api_bench_test.go` to that checkout's
`ui/api_bench_test.go`. Run the same commands there. The earlier PR fixtures
are committed at `4f9c3f6`; run them in a separate checkout. Keep compilation
and other tests out of the timed runs.
