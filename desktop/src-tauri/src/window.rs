use std::error::Error;
use std::sync::atomic::{AtomicU8, AtomicU64, Ordering};
use std::thread;
use std::time::Duration;

use serde::Serialize;
use tauri::webview::{NewWindowResponse, WebviewWindowBuilder};
use tauri::{AppHandle, Emitter, Manager, Url, WebviewWindow, WindowEvent};

use crate::{background, settings, tray};

const LABEL: &str = "main";
const OPEN: u8 = 0;
const HIDDEN: u8 = 1;
const BACKGROUND: u8 = 2;
const CLOSE_TIMEOUT: Duration = Duration::from_secs(3);

static MODE: AtomicU8 = AtomicU8::new(OPEN);
static PENDING_CLOSE: AtomicU64 = AtomicU64::new(0);
static CLOSE_REQUESTS: AtomicU64 = AtomicU64::new(0);

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct CloseRequest {
    keep_webview: bool,
    close_to_tray: bool,
}

fn is_app_url(url: &Url, dev_url: Option<&Url>) -> bool {
    match url.scheme() {
        "tauri" | "about" | "blob" | "data" => true,
        "http" | "https" => {
            url.host_str() == Some("tauri.localhost")
                || dev_url.is_some_and(|dev| dev.origin() == url.origin())
        }
        _ => false,
    }
}

fn open_outside(url: &Url) {
    if matches!(url.scheme(), "http" | "https" | "mailto") {
        let _ = tauri_plugin_opener::open_url(url.as_str(), None::<&str>);
    }
}

pub fn start(app: &AppHandle, minimized: bool) -> Result<Option<WebviewWindow>, Box<dyn Error>> {
    if minimized && !cfg!(windows) {
        MODE.store(BACKGROUND, Ordering::SeqCst);
        tray::show(app);
        return Ok(None);
    }
    let window = build_window(app, !minimized)?;
    if minimized {
        MODE.store(HIDDEN, Ordering::SeqCst);
        tray::show(app);
    }
    Ok(Some(window))
}

pub fn build(app: &AppHandle) -> Result<WebviewWindow, Box<dyn Error>> {
    build_window(app, true)
}

fn build_window(app: &AppHandle, visible: bool) -> Result<WebviewWindow, Box<dyn Error>> {
    let config = app
        .config()
        .app
        .windows
        .iter()
        .find(|window| window.label == LABEL)
        .cloned()
        .ok_or("tauri.conf.json defines no main window")?;
    let dev_url = if cfg!(dev) {
        app.config().build.dev_url.clone()
    } else {
        None
    };
    let window = WebviewWindowBuilder::from_config(app, &config)?
        .visible(visible)
        .on_navigation(move |url| {
            if is_app_url(url, dev_url.as_ref()) {
                return true;
            }
            open_outside(url);
            false
        })
        .on_new_window(|url, _| {
            open_outside(&url);
            NewWindowResponse::Deny
        })
        .build()?;
    let handle = app.clone();
    window.on_window_event(move |event| {
        if let WindowEvent::CloseRequested { api, .. } = event {
            api.prevent_close();
            request_close(&handle);
        }
    });
    MODE.store(OPEN, Ordering::SeqCst);
    Ok(window)
}

fn request_close(app: &AppHandle) {
    let request = CLOSE_REQUESTS.fetch_add(1, Ordering::SeqCst) + 1;
    PENDING_CLOSE.store(request, Ordering::SeqCst);
    let _ = app.emit_to(
        LABEL,
        "close-requested",
        CloseRequest {
            keep_webview: cfg!(windows),
            close_to_tray: settings::get(app).close_to_tray,
        },
    );
    let app = app.clone();
    thread::spawn(move || {
        thread::sleep(CLOSE_TIMEOUT);
        if PENDING_CLOSE
            .compare_exchange(request, 0, Ordering::SeqCst, Ordering::SeqCst)
            .is_ok()
            && let Some(window) = app.get_webview_window(LABEL)
        {
            let _ = window.destroy();
        }
    });
}

fn settle_close() {
    PENDING_CLOSE.store(0, Ordering::SeqCst);
}

pub fn keeps_running() -> bool {
    MODE.load(Ordering::SeqCst) != OPEN
}

pub fn enter_background(app: &AppHandle) {
    settle_close();
    MODE.store(BACKGROUND, Ordering::SeqCst);
    tray::show(app);
    if let Some(window) = app.get_webview_window(LABEL) {
        let _ = window.destroy();
    }
}

pub fn open(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(LABEL) {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
        MODE.store(OPEN, Ordering::SeqCst);
        return;
    }
    let app = app.clone();
    thread::spawn(move || {
        let _ = build(&app);
    });
}

pub fn quit(app: &AppHandle) {
    let app = app.clone();
    thread::spawn(move || {
        background::stop(&app);
        app.exit(0);
    });
}

#[tauri::command]
pub fn window_tray(app: AppHandle, window: WebviewWindow) {
    if cfg!(windows) {
        window_hide(app, window);
    } else {
        enter_background(&app);
    }
}

#[tauri::command]
pub fn app_quit(app: AppHandle) {
    settle_close();
    quit(&app);
}

#[tauri::command]
pub fn window_hide(app: AppHandle, window: WebviewWindow) {
    settle_close();
    MODE.store(HIDDEN, Ordering::SeqCst);
    let _ = window.hide();
    tray::show(&app);
}
