// GPUI's side of the comparison: the scenes of internal/gpuibench/app, element
// for element, written as GPUI apps write them. See ../README.md.
//
// A frame's view changes what the scene shows and asks for the next frame. It
// counts warmup frames, prints "ready", and once the driver answers on stdin
// counts the frames it measures, then prints what it measured as JSON.

use std::{
    io::{BufRead, Write},
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Instant,
};

use gpui::{
    AnyElement, App, Bounds, BoxShadow, Context, FontWeight, MouseMoveEvent, Rgba, ScrollHandle,
    ScrollStrategy, SharedString, UniformListScrollHandle, Window, WindowBounds, WindowOptions,
    div, point, prelude::*, px, rgb, size, uniform_list,
};
use gpui_platform::application;

#[link(name = "kernel32")]
unsafe extern "system" {
    fn GetCurrentProcess() -> isize;
    fn GetCurrentThread() -> isize;
    fn GetCurrentThreadId() -> u32;
    fn QueryProcessCycleTime(process: isize, cycles: *mut u64) -> i32;
    fn QueryThreadCycleTime(thread: isize, cycles: *mut u64) -> i32;
}

fn thread_cycles() -> u64 {
    let mut c = 0;
    unsafe { QueryThreadCycleTime(GetCurrentThread(), &mut c) };
    c
}

fn process_cycles() -> u64 {
    let mut c = 0;
    unsafe { QueryProcessCycleTime(GetCurrentProcess(), &mut c) };
    c
}

fn rgba8(r: usize, g: usize, b: usize, a: f32) -> Rgba {
    Rgba {
        r: r as f32 / 255.,
        g: g as f32 / 255.,
        b: b as f32 / 255.,
        a,
    }
}

fn shadow(y: f32, blur: f32, alpha: f32) -> Vec<BoxShadow> {
    vec![BoxShadow {
        color: rgba8(0, 0, 0, alpha).into(),
        offset: point(px(0.), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.),
        inset: false,
    }]
}

/// Measures frames, the same on both sides.
struct Meter {
    warmup: usize,
    want: usize,
    seen: usize,
    first: Option<Instant>,
    started: Arc<AtomicBool>,
    ready: bool,
    measuring: bool,
    done: bool,
    stamps: Vec<Instant>,
    thread0: u64,
    process0: u64,
}

impl Meter {
    fn new(warmup: usize, want: usize) -> Self {
        Meter {
            warmup,
            want,
            seen: 0,
            first: None,
            started: Arc::new(AtomicBool::new(false)),
            ready: false,
            measuring: false,
            done: false,
            stamps: Vec::new(),
            thread0: 0,
            process0: 0,
        }
    }

    fn tick(&mut self) {
        self.seen += 1;
        if self.done {
            return;
        }
        let first = *self.first.get_or_insert_with(Instant::now);
        // Scenes too heavy for many frames warm up for 5 seconds, and measure 12.
        if !self.ready && (self.seen >= self.warmup || first.elapsed().as_secs_f64() > 5.) {
            self.ready = true;
            println!("ready");
            std::io::stdout().flush().ok();
            let started = self.started.clone();
            std::thread::spawn(move || {
                let mut line = String::new();
                std::io::stdin().lock().read_line(&mut line).ok();
                started.store(true, Ordering::Release);
                // The driver ends this process when it has what it needs.
            });
        }
        if !self.measuring {
            if self.started.load(Ordering::Acquire) {
                self.measuring = true;
                self.stamps.reserve_exact(self.want + 1);
                self.stamps.push(Instant::now());
                self.thread0 = thread_cycles();
                self.process0 = process_cycles();
            }
            return;
        }
        self.stamps.push(Instant::now());
        let n = self.stamps.len() - 1;
        if n >= self.want || (n >= 30 && self.stamps[0].elapsed().as_secs_f64() > 12.) {
            let thread = thread_cycles() - self.thread0;
            let process = process_cycles() - self.process0;
            let intervals: Vec<String> = self
                .stamps
                .windows(2)
                .map(|w| format!("{:.3}", (w[1] - w[0]).as_secs_f64() * 1e3))
                .collect();
            println!(
                "{{\"frames\":{},\"thread_cycles\":{},\"process_cycles\":{},\"tid\":{},\"intervals_ms\":[{}]}}",
                n,
                thread,
                process,
                unsafe { GetCurrentThreadId() },
                intervals.join(",")
            );
            std::io::stdout().flush().ok();
            self.done = true;
        }
    }
}

struct Bench {
    scene: String,
    count: usize,
    frame: usize,
    meter: Meter,
    list: UniformListScrollHandle,
    scroll: ScrollHandle,
    right: bool,
    half: f32,
}

const WORDS: &str = "The quick brown fox jumps over the lazy dog while a sphinx of black quartz judges my vow, and five boxing wizards jump quickly past the pack of liquor jugs that sits beside the old stone wall at the end of a long and winding road through the hills.";

fn row(label: SharedString) -> impl IntoElement {
    div()
        .flex()
        .flex_none()
        .w_full()
        .items_center()
        .h(px(28.))
        .px(px(12.))
        .gap(px(8.))
        .child(div().flex_grow(1.).child(label))
        .child(div().text_color(rgb(0x6e6e78)).child("detail"))
}

impl Bench {
    fn boxes(&self, left: f32) -> Vec<AnyElement> {
        let frame = self.frame;
        (0..self.count)
            .map(|i| {
                let x = 8. + (i % 30) as f32 * 29. + (frame % 40) as f32 * 0.25;
                let y = 8. + (i / 30 % 20) as f32 * 31. + ((frame + i) % 20) as f32 * 0.5;
                let t = frame + i;
                div()
                    .absolute()
                    .left(px(left + x))
                    .top(px(y))
                    .size(px(24.))
                    .rounded(px(6.))
                    .shadow(shadow(2., 8., 0.27))
                    .bg(rgba8(
                        40 + t * 7 % 180,
                        80 + i * 13 % 150,
                        120 + frame * 3 % 120,
                        1.,
                    ))
                    .into_any_element()
            })
            .collect()
    }
}

impl Render for Bench {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.meter.tick();
        self.frame += 1;
        let frame = self.frame;
        let root = div()
            .relative()
            .size_full()
            .font_family("Segoe UI")
            .text_size(px(14.))
            .text_color(rgb(0x18181b))
            .line_height(gpui::relative(1.33));
        match self.scene.as_str() {
            "idle" => {
                return root
                    .flex()
                    .flex_col()
                    .items_center()
                    .justify_center()
                    .gap(px(16.))
                    .bg(rgb(0x7b2ff7))
                    .child(div().text_size(px(56.)).child("0"))
                    .child(
                        div()
                            .px(px(16.))
                            .py(px(8.))
                            .rounded(px(6.))
                            .bg(rgb(0xe5e7eb))
                            .child("Add"),
                    )
                    .into_any_element();
            }
            "input" | "input-busy" => {
                let busy = self.scene == "input-busy";
                if busy {
                    window.request_animation_frame();
                }
                let bg = if self.right {
                    rgb(0xffffff)
                } else {
                    rgb(0x000000)
                };
                let half = self.half;
                let root = root.id("root").bg(bg).on_mouse_move(cx.listener(
                    move |this, e: &MouseMoveEvent, _, cx| {
                        let right = f32::from(e.position.x) >= half;
                        if right != this.right {
                            this.right = right;
                            cx.notify();
                        }
                    },
                ));
                return if busy {
                    root.children(self.boxes(40.)).into_any_element()
                } else {
                    root.into_any_element()
                };
            }
            _ => {}
        }
        window.request_animation_frame();
        match self.scene.as_str() {
            "boxes" => root
                .bg(rgb(0xf6f7f9))
                .children(self.boxes(0.))
                .into_any_element(),
            "labels" => root
                .flex()
                .flex_col()
                .p(px(8.))
                .gap(px(4.))
                .bg(rgb(0xffffff))
                .children((0..20).map(|r| {
                    div().flex().gap(px(4.)).children((0..15).map(|col| {
                        div()
                            .w(px(54.))
                            .text_size(px(13.))
                            .child(format!("{}", (frame * 31 + r * 15 + col) * 7919 % 100000))
                    }))
                }))
                .into_any_element(),
            "translated_column" => root
                .bg(rgb(0xffffff))
                .overflow_hidden()
                .child(
                    div()
                        .absolute()
                        .w_full()
                        .top(px(-((frame % 2000) as f32) * 3.))
                        .flex()
                        .flex_col()
                        .children((0..1000).map(|i| row(format!("Row {i}").into()))),
                )
                .into_any_element(),
            "scrolled_column" => {
                self.scroll
                    .set_offset(point(px(0.), px(-((frame % 2000) as f32) * 3.)));
                root.bg(rgb(0xffffff))
                    .child(
                        div()
                            .id("scroll")
                            .size_full()
                            .flex()
                            .flex_col()
                            .overflow_y_scroll()
                            .track_scroll(&self.scroll)
                            .children((0..1000).map(|i| row(format!("Row {i}").into()))),
                    )
                    .into_any_element()
            }
            "list" => {
                self.list
                    .scroll_to_item_strict(frame * 3 % 99_000, ScrollStrategy::Top);
                root.bg(rgb(0xffffff))
                    .child(
                        uniform_list("rows", 100_000, |range, _, _| {
                            range
                                .map(|i| row(format!("Row {i}").into()))
                                .collect::<Vec<_>>()
                        })
                        .track_scroll(&self.list)
                        .size_full(),
                    )
                    .into_any_element()
            }
            "dashboard" => root
                .flex()
                .flex_col()
                .p(px(16.))
                .gap(px(16.))
                .bg(rgb(0xf2f3f6))
                .children((0..3).map(|r| {
                    div()
                        .flex()
                        .flex_1()
                        .items_center()
                        .gap(px(16.))
                        .children((0..4).map(|col| {
                            let i = r * 4 + col;
                            let v = (frame * (i + 3) + i * 17) % 1000;
                            div()
                                .flex()
                                .flex_col()
                                .flex_1()
                                .flex_basis(px(0.))
                                .p(px(14.))
                                .gap(px(8.))
                                .rounded(px(10.))
                                .bg(rgb(0xffffff))
                                .shadow(shadow(4., 16., 0.12))
                                .child(
                                    div()
                                        .text_size(px(12.))
                                        .text_color(rgb(0x6e6e78))
                                        .child(format!("Metric {}", i + 1)),
                                )
                                .child(
                                    div()
                                        .text_size(px(28.))
                                        .font_weight(FontWeight::BOLD)
                                        .child(format!("{}.{}%", v / 10, v % 10)),
                                )
                                .child(
                                    div()
                                        .w_full()
                                        .h(px(6.))
                                        .rounded(px(3.))
                                        .bg(rgb(0xd9d9de))
                                        .overflow_hidden()
                                        .child(
                                            div()
                                                .h_full()
                                                .w(gpui::relative(v as f32 / 1000.))
                                                .rounded(px(3.))
                                                .bg(rgb(0x2563eb)),
                                        ),
                                )
                                .child(div().text_size(px(12.)).child(format!(
                                    "{} requests, {} errors",
                                    v * 13,
                                    v % 7
                                )))
                        }))
                }))
                .into_any_element(),
            "paragraphs" => root
                .flex()
                .flex_col()
                .p(px(16.))
                .gap(px(8.))
                .bg(rgb(0xffffff))
                .text_size(px(13.))
                .children((0..self.count).map(|i| {
                    div()
                        .w_full()
                        .child(format!("{} {i}: {WORDS}", frame * (i + 1)))
                }))
                .into_any_element(),
            other => panic!("no scene {other}"),
        }
    }
}

fn flag(args: &[String], name: &str, default: &str) -> String {
    args.iter()
        .position(|a| a == name)
        .and_then(|i| args.get(i + 1).cloned())
        .unwrap_or_else(|| default.to_string())
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let num = |name, default: &str| flag(&args, name, default).parse::<usize>().unwrap();
    let scene = flag(&args, "-scene", "boxes");
    let count = num("-count", "600");
    let warmup = num("-warmup", "120");
    let frames = num("-frames", "600");
    let (x, y) = (num("-x", "40") as f32, num("-y", "40") as f32);
    let (w, h) = (num("-w", "900") as f32, num("-h", "640") as f32);
    application().run(move |cx: &mut App| {
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(Bounds {
                    origin: point(px(x), px(y)),
                    size: size(px(w), px(h)),
                })),
                focus: true,
                // GPUI draws windows without the keyboard, and on battery, at
                // 30 Hz unless told otherwise; MyGo does not.
                inactive_frame_interval: None,
                low_power_frame_interval: None,
                ..Default::default()
            },
            move |_, cx| {
                cx.new(|_| Bench {
                    scene,
                    count,
                    frame: 0,
                    meter: Meter::new(warmup, frames),
                    list: UniformListScrollHandle::new(),
                    scroll: ScrollHandle::new(),
                    right: false,
                    half: w / 2.,
                })
            },
        )
        .unwrap();
        cx.activate(true);
    });
}
