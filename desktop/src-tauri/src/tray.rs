use std::sync::Mutex;

use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager, Wry};

use crate::audio::{Audio, Msg};
use crate::window;

const ID: &str = "main";

pub struct Tray(Mutex<State>);

struct State {
    items: Option<Items>,
    now: String,
    playing: bool,
}

struct Items {
    now: MenuItem<Wry>,
    toggle: MenuItem<Wry>,
}

impl Default for Tray {
    fn default() -> Self {
        Tray(Mutex::new(State {
            items: None,
            now: "Music".into(),
            playing: false,
        }))
    }
}

fn toggle_label(playing: bool) -> &'static str {
    if playing { "Pause" } else { "Play" }
}

pub fn show(app: &AppHandle) {
    if let Some(tray) = app.tray_by_id(ID) {
        let _ = tray.set_visible(true);
        return;
    }
    let _ = build(app);
}

fn build(app: &AppHandle) -> tauri::Result<()> {
    let (label, playing) = app
        .state::<Tray>()
        .0
        .lock()
        .map(|state| (state.now.clone(), state.playing))
        .unwrap_or_else(|_| ("Music".into(), false));
    let now = MenuItem::with_id(app, "now", label, false, None::<&str>)?;
    let toggle = MenuItem::with_id(app, "toggle", toggle_label(playing), true, None::<&str>)?;
    let next = MenuItem::with_id(app, "next", "Next", true, None::<&str>)?;
    let previous = MenuItem::with_id(app, "previous", "Previous", true, None::<&str>)?;
    let open = MenuItem::with_id(app, "open", "Open Music", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
    let menu = Menu::with_items(
        app,
        &[
            &now,
            &PredefinedMenuItem::separator(app)?,
            &toggle,
            &next,
            &previous,
            &PredefinedMenuItem::separator(app)?,
            &open,
            &quit,
        ],
    )?;
    let mut builder = TrayIconBuilder::with_id(ID)
        .tooltip("Music")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "toggle" => media(app, "toggle"),
            "next" => media(app, "next"),
            "previous" => media(app, "previous"),
            "open" => window::open(app),
            "quit" => window::quit(app),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                window::open(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    if let Ok(mut state) = app.state::<Tray>().0.lock() {
        state.items = Some(Items { now, toggle });
    }
    Ok(())
}

fn media(app: &AppHandle, action: &str) {
    let _ = app
        .state::<Audio>()
        .sender()
        .send(Msg::Media(action.into(), None));
}

pub fn now_playing(app: &AppHandle, title: &str, artist: Option<&str>) {
    let label = match artist {
        Some(artist) if !artist.is_empty() => format!("{title} — {artist}"),
        _ => title.to_string(),
    };
    if let Ok(mut state) = app.state::<Tray>().0.lock() {
        state.now = label;
    }
    refresh(app);
}

pub fn playing(app: &AppHandle, playing: bool) {
    let changed = app
        .state::<Tray>()
        .0
        .lock()
        .map(|mut state| std::mem::replace(&mut state.playing, playing) != playing)
        .unwrap_or(false);
    if changed {
        refresh(app);
    }
}

fn refresh(app: &AppHandle) {
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Ok(state) = handle.state::<Tray>().0.lock()
            && let Some(items) = state.items.as_ref()
        {
            let _ = items.now.set_text(&state.now);
            let _ = items.toggle.set_text(toggle_label(state.playing));
        }
    });
}
