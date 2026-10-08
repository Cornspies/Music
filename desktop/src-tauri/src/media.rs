use std::cell::RefCell;
use std::ffi::c_void;
use std::time::Duration;

use crossbeam_channel::Sender;
use souvlaki::{
    MediaControlEvent, MediaControls, MediaMetadata, MediaPlayback, MediaPosition, PlatformConfig,
    SeekDirection,
};
use tauri::{AppHandle, WebviewWindow};

use crate::audio::Msg;

thread_local! {
    static CONTROLS: RefCell<Option<MediaControls>> = const { RefCell::new(None) };
}

struct Metadata {
    title: String,
    artist: Option<String>,
    album: Option<String>,
    cover_url: Option<String>,
    duration: Option<f64>,
}

pub fn setup(app: &AppHandle, window: Option<&WebviewWindow>, audio: Sender<Msg>) {
    let config = PlatformConfig {
        display_name: "Music",
        dbus_name: "music_desktop",
        hwnd: window.and_then(hwnd),
    };
    let Ok(mut controls) = MediaControls::new(config) else {
        return;
    };
    let app = app.clone();
    if controls
        .attach(move |event| handle(&app, &audio, event))
        .is_err()
    {
        return;
    }
    CONTROLS.with(|cell| *cell.borrow_mut() = Some(controls));
}

fn handle(app: &AppHandle, audio: &Sender<Msg>, event: MediaControlEvent) {
    let send = |action: &str, position: Option<f64>| {
        let _ = audio.send(Msg::Media(action.into(), position));
    };
    match event {
        MediaControlEvent::Play => send("play", None),
        MediaControlEvent::Pause | MediaControlEvent::Stop => send("pause", None),
        MediaControlEvent::Toggle => send("toggle", None),
        MediaControlEvent::Next => send("next", None),
        MediaControlEvent::Previous => send("previous", None),
        MediaControlEvent::Seek(direction) => send("seekBy", Some(signed(direction, 10.0))),
        MediaControlEvent::SeekBy(direction, by) => {
            send("seekBy", Some(signed(direction, by.as_secs_f64())))
        }
        MediaControlEvent::SetPosition(MediaPosition(at)) => send("seekTo", Some(at.as_secs_f64())),
        MediaControlEvent::Raise => crate::window::open(app),
        _ => {}
    }
}

fn signed(direction: SeekDirection, seconds: f64) -> f64 {
    match direction {
        SeekDirection::Forward => seconds,
        SeekDirection::Backward => -seconds,
    }
}

fn with_controls(app: &AppHandle, update: impl FnOnce(&mut MediaControls) + Send + 'static) {
    let _ = app.run_on_main_thread(move || {
        CONTROLS.with(|cell| {
            if let Some(controls) = cell.borrow_mut().as_mut() {
                update(controls);
            }
        });
    });
}

pub fn metadata(
    app: &AppHandle,
    title: String,
    artist: Option<String>,
    album: Option<String>,
    cover_url: Option<String>,
    duration: Option<f64>,
) {
    crate::tray::now_playing(app, &title, artist.as_deref());
    crate::discord::track(&title, artist.as_deref(), album.as_deref(), duration);
    let meta = Metadata {
        title,
        artist,
        album,
        cover_url,
        duration: duration.filter(|d| d.is_finite() && *d > 0.0),
    };
    with_controls(app, move |controls| {
        let _ = controls.set_metadata(MediaMetadata {
            title: Some(&meta.title),
            artist: meta.artist.as_deref(),
            album: meta.album.as_deref(),
            cover_url: meta.cover_url.as_deref(),
            duration: meta.duration.map(Duration::from_secs_f64),
        });
    });
}

pub fn playback(app: &AppHandle, state: Option<(bool, f64)>) {
    crate::tray::playing(app, matches!(state, Some((true, _))));
    crate::discord::playback(state);
    with_controls(app, move |controls| {
        let playback = match state {
            None => MediaPlayback::Stopped,
            Some((playing, position)) => {
                let progress = Some(MediaPosition(Duration::from_secs_f64(position.max(0.0))));
                if playing {
                    MediaPlayback::Playing { progress }
                } else {
                    MediaPlayback::Paused { progress }
                }
            }
        };
        let _ = controls.set_playback(playback);
    });
}

pub fn clear(app: &AppHandle) {
    crate::tray::now_playing(app, "Music", None);
    crate::tray::playing(app, false);
    crate::discord::playback(None);
    with_controls(app, |controls| {
        let _ = controls.set_playback(MediaPlayback::Stopped);
        let _ = controls.set_metadata(MediaMetadata::default());
    });
}

#[tauri::command]
pub fn media_metadata(
    app: AppHandle,
    title: String,
    artist: Option<String>,
    album: Option<String>,
    cover_url: Option<String>,
    duration: Option<f64>,
) {
    metadata(&app, title, artist, album, cover_url, duration);
}

#[cfg(windows)]
fn hwnd(window: &WebviewWindow) -> Option<*mut c_void> {
    window.hwnd().ok().map(|handle| handle.0 as *mut c_void)
}

#[cfg(not(windows))]
fn hwnd(_: &WebviewWindow) -> Option<*mut c_void> {
    None
}
