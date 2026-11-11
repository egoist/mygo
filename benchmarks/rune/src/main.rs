//! Rune's app for the comparison of benchmarks/README.md, with the scenes the
//! other frameworks' apps have. The list scrolls from `poll`, outside the
//! view, as a wheel does, and the other scenes commit their changes at once
//! (`Tx::instant`), as Rune springs a change in layout or colour otherwise.

#[path = "../../common.rs"]
mod common;

use std::borrow::Cow;
use std::time::Duration;

use common::{Recorder, cell_color, cell_value, document_width, words};
use rune::color::srgb_decode;
use rune::{
    App, AppSpec, Chrome, Color, Cx, El, FrameOutcome, GrowthSpec, ScrollSpec, TextStyle, Tx, col, el, row, text,
};

fn color((r, g, b): (u8, u8, u8)) -> Color {
    let c = |v: u8| srgb_decode(v as f32 / 255.0);
    Color {
        r: c(r),
        g: c(g),
        b: c(b),
        a: 1.0,
    }
}

fn style(size: f32) -> TextStyle {
    TextStyle::new(size, 400.0)
}

#[derive(Default)]
struct Counter {
    n: i32,
}

fn button(label: &str, press: fn(&mut Counter)) -> El {
    el().px(12.0)
        .py(4.0)
        .radius(6.0)
        .fill(color((0xe8, 0xe8, 0xe8)))
        .child(text(label).font(style(13.0)).color(Color::BLACK))
        .on_press(press)
}

fn counter_view(s: &Counter, _: &mut Cx<Counter>) -> El {
    col()
        .w_full()
        .h_full()
        .center()
        .gap(16.0)
        .fill(Color::WHITE)
        .child(
            text(s.n.to_string())
                .font(TextStyle::new(56.0, 700.0))
                .color(Color::BLACK),
        )
        .child(
            row()
                .gap(8.0)
                .child(button("−", |s| s.n -= 1))
                .child(button("Reset", |s| s.n = 0))
                .child(button("+", |s| s.n += 1)),
        )
}

struct List {
    rec: Recorder,
    y: f32,
}

fn list_row(_: &List, i: usize) -> El {
    row()
        .w_full()
        .h(40.0)
        .px(12.0)
        .gap(12.0)
        .center_y()
        .child(
            col()
                .grow(1.0)
                .child(text(format!("Item {i}")).font(style(13.0)).color(Color::BLACK))
                .child(text(words(i + 1, 6)).font(style(11.0)).color(color((0x70, 0x70, 0x70)))),
        )
        .child(
            text(format!("{:.2}", cell_value(i, 0)))
                .font(style(13.0))
                .color(Color::BLACK),
        )
}

fn list_view(_: &List, _: &mut Cx<List>) -> El {
    col()
        .key("list")
        .w_full()
        .h_full()
        .fill(Color::WHITE)
        .scroll(ScrollSpec { stick_bottom: false })
        .child(
            // Rows built by index, only those near the viewport; the keys never change.
            col()
                .key("rows")
                .w_full()
                .growth(GrowthSpec::default())
                .virtual_rows(100_000, 40.0, |_: &List, i| Cow::Owned(i.to_string()), list_row)
                .virtual_rows_version(0),
        )
}

/// Scrolls the list once a frame, which moves its content without running the view.
fn list_poll(s: &mut List, _: f64, tx: &mut Tx) -> bool {
    if s.rec.tick() {
        s.y += 8.0;
        tx.scroll_to_y("list", s.y);
        tx.wake_after(Duration::ZERO);
    }
    false
}

struct Grid {
    rec: Recorder,
    frame: usize,
}

fn grid_view(s: &Grid, _: &mut Cx<Grid>) -> El {
    col().w_full().h_full().fill(Color::WHITE).children((0..30).map(|r| {
        row().children((0..20).map(move |c| {
            let v = cell_value(r * 20 + c, s.frame);
            el().size(40.0, 20.0).center().fill(color(cell_color(v))).child(
                text(format!("{v:.2}"))
                    .font(style(11.0))
                    .color(color((0x10, 0x10, 0x10))),
            )
        }))
    }))
}

fn grid_poll(s: &mut Grid, _: f64, tx: &mut Tx) -> bool {
    if !s.rec.tick() {
        return false;
    }
    s.frame += 1;
    tx.instant();
    tx.wake_after(Duration::ZERO);
    true
}

struct Document {
    rec: Recorder,
    paragraphs: Vec<String>,
    frame: usize,
}

fn document_view(s: &Document, _: &mut Cx<Document>) -> El {
    col()
        .w_full()
        .h_full()
        .fill(Color::WHITE)
        .scroll(ScrollSpec { stick_bottom: false })
        .child(
            col().w(document_width(s.frame)).padding(12.0).gap(8.0).children(
                s.paragraphs
                    .iter()
                    .map(|p| text(p.clone()).font(style(13.0)).color(Color::BLACK).wrap().w_full()),
            ),
        )
}

fn document_poll(s: &mut Document, _: f64, tx: &mut Tx) -> bool {
    if !s.rec.tick() {
        return false;
    }
    s.frame += 1;
    tx.instant();
    tx.wake_after(Duration::ZERO);
    true
}

/// An app of a window of `size`, as the other frameworks open it: titled, of
/// that size at least, and given none of the process's arguments, which are
/// the runner's.
fn app<S: 'static>(spec: AppSpec<S>) -> App<S> {
    let size = spec.size;
    App::new(AppSpec {
        min_size: Some(size),
        ..spec
    })
    .args(Vec::new())
    .chrome(Chrome {
        full_size_content: false,
        ..Chrome::DEFAULT
    })
}

fn main() {
    // What Rune's guide to launching fast asks of every app: the GPU device
    // and pipeline are made while AppKit starts.
    rune::prewarm_gpu();
    let scene = std::env::args().nth(1).unwrap_or_else(|| "counter".into());
    let small = (360.0, 260.0);
    let large = (800.0, 600.0);
    match scene.as_str() {
        "counter" => {
            let mut ready = false;
            app(AppSpec::new("Benchmark", small, Counter::default, counter_view))
                .on_frame_encoded(move |f| {
                    if !ready && matches!(f.outcome(), FrameOutcome::Drawn) {
                        ready = true;
                        println!("ready");
                    }
                })
                .run();
        }
        "list" => app(AppSpec {
            poll: Some(list_poll),
            ..AppSpec::new(
                "Benchmark",
                large,
                || List {
                    rec: Recorder::new(),
                    y: 0.0,
                },
                list_view,
            )
        })
        .run(),
        "grid" => app(AppSpec {
            poll: Some(grid_poll),
            ..AppSpec::new(
                "Benchmark",
                large,
                || Grid {
                    rec: Recorder::new(),
                    frame: 0,
                },
                grid_view,
            )
        })
        .run(),
        "document" => app(AppSpec {
            poll: Some(document_poll),
            ..AppSpec::new(
                "Benchmark",
                large,
                || Document {
                    rec: Recorder::new(),
                    paragraphs: (0..40).map(|i| words(i + 1, 70)).collect(),
                    frame: 0,
                },
                document_view,
            )
        })
        .run(),
        other => panic!("no scene {other:?}"),
    }
}
