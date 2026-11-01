// The smallest app worth measuring, as internal/gpuibench/hello: a number and
// a box that counts when clicked. Its size is what an app pays for GPUI.
#![windows_subsystem = "windows"]

use gpui::{
    App, Bounds, Context, Window, WindowBounds, WindowOptions, div, prelude::*, px, rgb, size,
};
use gpui_platform::application;

struct Hello {
    n: usize,
}

impl Render for Hello {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .flex()
            .flex_col()
            .size_full()
            .items_center()
            .justify_center()
            .gap(px(16.))
            .bg(rgb(0xffffff))
            .text_color(rgb(0x000000))
            .child(div().text_size(px(56.)).child(format!("{}", self.n)))
            .child(
                div()
                    .id("add")
                    .px(px(16.))
                    .py(px(8.))
                    .rounded(px(6.))
                    .bg(rgb(0xe5e7eb))
                    .child("Add")
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.n += 1;
                        cx.notify();
                    })),
            )
    }
}

fn main() {
    application().run(|cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(360.), px(260.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| cx.new(|_| Hello { n: 0 }),
        )
        .unwrap();
    });
}
