# Checked UI API performance

Measured on October 8, 2026 on an Apple M5, macOS arm64, Go 1.27.1,
with `CGO_ENABLED=0`. The baseline is commit `fab604b` (MyGo 0.2.18).

The fixtures build the same interface through the earlier pointer API and
the checked value API. `BenchmarkValueFrame` builds 1,000 static text
elements. `BenchmarkValueList` shows the visible rows of a million-row
list, formatting their labels in its builder. Both warm the view first.
Frames render through `ui.Tester` on the CPU. The runs were sequential,
with `GOMAXPROCS=1`, six samples per fixture and 300 ms per sample. Values
below are medians.

| Fixture | Earlier API | Checked API | Change |
|---|---:|---:|---:|
| 1,000 labels: time/frame | 400 µs | 429 µs | +7.4% |
| 1,000 labels: allocations/frame | 0 | 0 | unchanged |
| 1,000 labels: allocated bytes/frame | 0 | 0 | unchanged |
| Virtual list: time/frame | 14.4 µs | 15.4 µs | +7.1% |
| Virtual list: allocations/frame | 20 | 23 | +3 |
| Virtual list: allocated bytes/frame | 144 | 280 | +136 |

The checked API keeps nodes pooled. Element and frame handles themselves
do not allocate. The list's additional allocations come from its deferred
configuration and row callbacks. Most allocations in this fixture are
formatted row labels.

`TestValueFrameRetainedHeap` warms the fonts and renderer, releases a warm
view, forces collection, then measures a second retained view after ten
frames and another collection. Three samples measured approximately
2.43 MB of retained Go heap in both versions. This is Go heap, including
the headless rendering host; it is not native window process memory and
does not measure GPU resources.

Earlier exploratory runs varied substantially while compilation and other
tests ran. The table uses the final isolated runs. Small timing differences
should be rechecked on CI and other machines; the allocation counts are
more stable. These measurements cover steady frames, not a claim about
every widget or interaction.

Reproduce the checked fixtures:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 go test ./ui -run '^$' \
  -bench '^(BenchmarkValueFrame|BenchmarkValueList)$' \
  -benchmem -count=6 -benchtime=300ms
CGO_ENABLED=0 go test ./ui -run '^TestValueFrameRetainedHeap$' -count=3 -v
```

The baseline fixture changes only the view signature and callback syntax,
using the earlier four-argument `List` constructor. The rest of the data,
dimensions, styles, warm-up and measurement loop is the same.
