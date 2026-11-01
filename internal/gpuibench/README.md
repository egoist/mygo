# MyGo and GPUI compared

`gpuibench` draws the same scenes with MyGo's native UI and with
[GPUI](https://github.com/zed-industries/zed/tree/main/crates/gpui), Zed's
framework, in real windows on the real GPU and display, and measures both from
the outside in the same way.

```sh
go run ./internal/gpuibench                    # everything, about an hour, with windows on top of the others
go run ./internal/gpuibench -runs 2 -modes screen      # do the frames the apps count reach the display?
go run ./internal/gpuibench -modes shots -shots dir    # PNGs of every scene on both sides, to compare by eye
```

It needs Windows with a desktop session, Go, and Rust with the MSVC tools; the
first build downloads Zed (1.2 GB) and takes five minutes. `CARGO_TARGET_DIR`
says where Cargo builds. `results.json` holds the raw results below.

## Results

Measured on 2 November 2026 on Windows 11 25H2, a Ryzen 7 8745HS with its
Radeon 780M (driver 32.0.12019.1028), a 3840×2160 display at 59.94 Hz and scale
2, on the Balanced power plan. MyGo at 1ee84625 with Go 1.27.3; GPUI at d94f32a1
with Rust 1.98.1.

Each figure is the median of six runs, alternating frameworks. From run to run
the CPU and GPU figures moved by under 10%, and a whole second pass gave them
again to within 6% (`results.json` has every run of the last).

### Frames

Windows of 900×640 DIPs (1800×1280 pixels) whose every frame changes all they
show. CPU is the cycles of all the process's threads, GPU the running time of
its 3D engine, each a frame; memory is the private working set at the end.

| Scene | fps MyGo | fps GPUI | CPU ms MyGo | CPU ms GPUI | GPU ms MyGo | GPU ms GPUI | MB MyGo | MB GPUI |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `boxes`: 600 with shadows, moving | 59.9 | 59.9 | **2.21** | 2.74 | 7.02 | **4.41** | 32 | **25** |
| `labels`: 300 changing numbers | 59.9 | 59.9 | **3.23** | 3.99 | 1.48 | **0.63** | 54 | **26** |
| `translated_column`: 1000 rows, moved | **59.9** | 46.3 | **5.97** | 21.83 | 1.26 | **0.54** | 65 | **46** |
| `scrolled_column`: 1000 rows, scrolled | **59.9** | 45.9 | **5.88** | 21.99 | 1.28 | **0.53** | 68 | **46** |
| `list`: virtual list of 100,000 rows | 59.9 | 59.9 | 2.59 | **1.41** | **0.39** | 0.50 | 41 | **22** |
| `dashboard`: 12 cards | 59.9 | 59.9 | 2.62 | **1.90** | 2.58 | **1.60** | 48 | **23** |
| `dashboard large`: at 3600×2000 pixels | 59.9 | 59.9 | 2.69 | **2.26** | 5.58 | **2.95** | 48 | **23** |
| `paragraphs`: 24 wrapped, reshaped | 59.9 | 59.9 | 4.41 | **2.91** | 1.71 | **0.68** | 90 | **24** |
| `boxes×4`: 2,400 | 29.9 | **59.9** | **4.89** | 10.13 | 23.38 | **11.07** | 46 | **37** |
| `boxes×16`: 9,600 | **20.0** | 16.7 | **20.23** | 60.86 | **35.11** | 41.01 | 110 | **87** |
| `boxes×64`: 38,400 | **7.4** | 3.5 | **84.64** | 290.44 | **129.54** | 151.35 | 410 | **273** |

- **MyGo builds big scenes for much less CPU**: 2,400 to 38,400 boxes take a
  half to under a third of GPUI's, and 1,000 rows take 5.9 ms, where GPUI's
  22 ms miss more than a fifth of the display's refreshes. GPUI takes less for the list,
  the dashboard and paragraphs.
- **GPUI draws for less GPU**, up to 2.5 times less, in every scene but the list
  and the two largest. It decides `boxes×4`, where MyGo's frames take 23 ms of
  the GPU and fall to every other refresh, as GPUI's 11 ms do not. This is MyGo's
  clearest weakness here, and why was not looked into.
- **GPUI takes less memory** in every scene, 1.2 to 3.8 times less.

The fps and CPU figures agree with each other where a side is saturated (GPUI's
`translated_column`: 21.6 ms on the main thread, a frame every 21.6 ms), and the
GPU's with the fps where the GPU is (MyGo's `boxes×64`: 130 ms, a frame every
135 ms).

With the display watched as well (`-modes screen`, two runs, which itself costs
each run a copy of the desktop), 98% or more of the frames each side counted
showed as new images, on both sides and in every scene. A first pass had MyGo's
as low as 96% at 60 fps, which this one did not repeat.

### Input to display

Pointer moves posted to the window under the pointer that change the window's
color, timed to the desktop image showing the new color reaching the display
(`LastPresentTime` of the Desktop Duplication API). 600 moves each, at any time
within a refresh.

| Scene | | mean ms | p50 ms | p95 ms | p99 ms |
|---|---|---:|---:|---:|---:|
| `input`: the window idle until the move | MyGo | **14.5** | 15.0 | 22.5 | 23.8 |
| | GPUI | 26.9 | 26.9 | 34.5 | 35.9 |
| `input-busy`: 300 boxes moving as well | MyGo | 43.6 | 43.9 | 51.5 | 53.5 |
| | GPUI | **24.5** | 23.7 | 32.3 | 33.4 |

Idle, MyGo shows a move a refresh sooner; while animating, GPUI does, by a
refresh and a bit. The likely reasons, from reading the code and not measured
apart: MyGo draws a frame at once after a pause, where GPUI's come from a thread
waiting for refreshes (`DwmFlush`); and when animating, one of MyGo's frames
waits ahead of the screen ("Input latency" in the architecture guide), where GPUI
presents without waiting (`Present(0, …)`).

### Start, idle and size

| | MyGo | GPUI |
|---|---:|---:|
| Process start to first frame on the display, ms (median of 11) | **180** | 259 |
| Private working set, idle, MB | 24.4 | **21.1** |
| Working set, idle, MB | 59.6 | **54.5** |
| Committed, idle, MB | **85.2** | 122.0 |
| CPU, idle, % of a core | **0.007** | 0.859 |
| Release build of `hello`, MiB | 9.4 | **8.5** |

Memory is read six seconds after the start, as `internal/idlemem` does, and
idle CPU over the next twenty seconds, with the window showing and nothing
changing. GPUI's thread waiting for refreshes runs all the while.

## What this does and does not say

- One machine, with an integrated GPU, on Windows at 60 Hz: no faster display, no
  discrete GPU, no macOS (where GPUI draws with Metal), no Linux.
- Most scenes are MyGo's own stress scenes (`ui/stress_bench_test.go`), which
  rebuild everything every frame. GPUI's ways of not doing that (`cached` views)
  are not used, and nothing here resembles a code editor. `paragraphs`,
  `scrolled_column` and the `input` scenes were written for this comparison.
- GPUI's scenes were written by MyGo's side. `-modes shots` shows them the same,
  but for GPUI's shadows being darker from the same parameters. `div` has no
  translation, so `translated_column` moves its column with `top`;
  `scrolled_column`, in a real scroll view, costs GPUI the same.
- GPUI draws windows without the keyboard at 30 Hz by default. Here it is told
  not to (`inactive_frame_interval: None`), as MyGo does not.
- Moves are posted to the window's queue, leaving out the system's input
  thread, and stop at the display's refresh, leaving out the panel. Both are
  the same on both sides. Duplicating the desktop runs meanwhile. Something on
  the desktop can take a move on its way, which fails the run: the driver says
  so and runs it again. One run of GPUI's did, in a pass whose latencies are not
  kept here; none did in the one that is.
- GPU time leaves out what the window manager spends compositing. Both draw
  through a flip-model swap chain.
- The machine was in use as a desktop: other processes, the window manager
  among them, took 1 to 1.5 cores during the runs, alike on both sides.

## How it works

- `main_windows.go` and `win_windows.go` are the driver. It builds both sides
  as releases: MyGo as `bench.ts` does (`-trimpath -tags mygo_noinspector
  -ldflags "-s -w -X …production=1"`), GPUI with Zed's release profile (thin LTO,
  one codegen unit) but stripped. It fails unless a window's content is the size
  asked for.
- `app` and `gpui/src/main.rs` are the two sides, with the same meter: warm up
  (120 frames or 5 seconds), say `ready`, and once the driver answers, measure
  600 frames or 12 seconds. They read `QueryThreadCycleTime` and
  `QueryProcessCycleTime`, since Windows' CPU times only count clock ticks; the
  driver turns cycles into time by spinning on a thread of its own.
- The GPU's time is the driver's, from the `GPU Engine\Running Time` counter,
  as Task Manager's.
- `hello` and `gpui/src/bin/hello.rs` are the smallest apps worth measuring.
