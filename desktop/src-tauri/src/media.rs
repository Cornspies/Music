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

#[derive(Clone)]
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
    #[cfg(target_os = "linux")]
    art::show(app, meta);
    #[cfg(not(target_os = "linux"))]
    with_controls(app, move |controls| set_metadata(controls, &meta));
}

fn set_metadata(controls: &mut MediaControls, meta: &Metadata) {
    let _ = controls.set_metadata(MediaMetadata {
        title: Some(&meta.title),
        artist: meta.artist.as_deref(),
        album: meta.album.as_deref(),
        cover_url: meta.cover_url.as_deref(),
        duration: meta.duration.map(Duration::from_secs_f64),
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
    #[cfg(target_os = "linux")]
    art::forget(app);
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

// MPRIS shows the art's address to every program on the session bus, and the
// server's address for a cover carries the media token. So the cover is saved
// as a file and that file's address is shown instead.
#[cfg(target_os = "linux")]
mod art {
    use std::fs;
    use std::path::{Path, PathBuf};
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::{Mutex, MutexGuard, OnceLock};
    use std::thread;

    use tauri::{AppHandle, Manager, Url};
    use ureq::Agent;

    use super::Metadata;

    const FOLDER: &str = "covers";
    const MAX_BYTES: u64 = 8 * 1024 * 1024;

    // Goes up with everything that's shown, so a cover that arrives late
    // isn't put on a newer track or left behind in the folder.
    static TURN: AtomicU64 = AtomicU64::new(0);
    static FILES: Mutex<()> = Mutex::new(());
    static AGENT: OnceLock<Agent> = OnceLock::new();

    #[derive(Debug, PartialEq)]
    struct Cover {
        track: String,
        file: String,
    }

    impl Cover {
        fn parse(url: &str) -> Option<Cover> {
            let url = Url::parse(url).ok()?;
            if !matches!(url.scheme(), "http" | "https") {
                return None;
            }
            let track = url
                .path()
                .rsplit_once("/api/tracks/")?
                .1
                .strip_suffix("/cover")?;
            if track.is_empty() || track.len() > 64 || !track.chars().all(plain) {
                return None;
            }
            let version: String = url
                .query_pairs()
                .find(|(key, _)| key == "v")
                .map(|(_, version)| version.chars().filter(|c| plain(*c)).take(32).collect())
                .unwrap_or_default();
            let file = if version.is_empty() {
                track.to_string()
            } else {
                format!("{track}.{version}")
            };
            Some(Cover {
                track: track.to_string(),
                file,
            })
        }
    }

    fn plain(c: char) -> bool {
        c.is_ascii_alphanumeric() || c == '-'
    }

    fn downloaded(offline: Option<PathBuf>, track: &str) -> Option<PathBuf> {
        let file = offline?.join(format!("{track}.cover"));
        file.is_file().then_some(file)
    }

    fn address(file: &Path) -> Option<String> {
        Url::from_file_path(file).ok().map(String::from)
    }

    fn sweep(folder: &Path, keep: Option<&str>) {
        let Ok(entries) = fs::read_dir(folder) else {
            return;
        };
        for entry in entries.flatten() {
            if keep.is_none_or(|keep| entry.file_name() != keep) {
                let _ = fs::remove_file(entry.path());
            }
        }
    }

    fn fetch(url: &str, to: &Path) -> Option<()> {
        let agent = AGENT.get_or_init(crate::audio::http_agent);
        let mut response = agent.get(url).call().ok()?;
        let image = response
            .body()
            .mime_type()
            .is_some_and(|mime| mime.starts_with("image/"));
        if response.status().as_u16() != 200 || !image {
            return None;
        }
        let bytes = response
            .body_mut()
            .with_config()
            .limit(MAX_BYTES)
            .read_to_vec()
            .ok()?;
        if bytes.is_empty() {
            return None;
        }
        fs::write(to, bytes).ok()
    }

    fn files() -> MutexGuard<'static, ()> {
        FILES.lock().unwrap_or_else(|e| e.into_inner())
    }

    fn current(turn: u64) -> bool {
        TURN.load(Ordering::SeqCst) == turn
    }

    fn folder(app: &AppHandle) -> Option<PathBuf> {
        app.path().app_cache_dir().ok().map(|dir| dir.join(FOLDER))
    }

    fn publish(app: &AppHandle, meta: Metadata, turn: u64) {
        super::with_controls(app, move |controls| {
            if current(turn) {
                super::set_metadata(controls, &meta);
            }
        });
    }

    pub fn show(app: &AppHandle, mut meta: Metadata) {
        let turn = TURN.fetch_add(1, Ordering::SeqCst) + 1;
        let url = meta.cover_url.take();
        let worker = {
            let app = app.clone();
            let meta = meta.clone();
            move || work(&app, meta, url, turn)
        };
        if thread::Builder::new()
            .name("cover".into())
            .spawn(worker)
            .is_err()
        {
            publish(app, meta, turn);
        }
    }

    pub fn forget(app: &AppHandle) {
        let turn = TURN.fetch_add(1, Ordering::SeqCst) + 1;
        let folder = folder(app);
        let _ = thread::Builder::new().name("cover".into()).spawn(move || {
            let _files = files();
            if let Some(folder) = folder
                && current(turn)
            {
                sweep(&folder, None);
            }
        });
    }

    fn work(app: &AppHandle, mut meta: Metadata, url: Option<String>, turn: u64) {
        let cover = url.as_deref().and_then(Cover::parse);
        let folder = folder(app);
        let saved = cover
            .as_ref()
            .zip(folder.as_ref())
            .map(|(cover, folder)| folder.join(&cover.file));
        let ready = {
            let _files = files();
            if !current(turn) {
                return;
            }
            if let Some(folder) = &folder {
                sweep(folder, cover.as_ref().map(|cover| cover.file.as_str()));
            }
            cover
                .as_ref()
                .and_then(|cover| downloaded(crate::offline::dir(), &cover.track))
                .or_else(|| saved.clone().filter(|file| file.is_file()))
        };
        if let Some(file) = ready {
            meta.cover_url = address(&file);
            publish(app, meta, turn);
            return;
        }
        publish(app, meta.clone(), turn);
        let (Some(url), Some(folder), Some(saved)) = (url, folder, saved) else {
            return;
        };
        let part = saved.with_added_extension(format!("{turn}.part"));
        let fetched = fs::create_dir_all(&folder).is_ok() && fetch(&url, &part).is_some();
        let _files = files();
        if fetched && current(turn) && fs::rename(&part, &saved).is_ok() {
            meta.cover_url = address(&saved);
            publish(app, meta, turn);
        } else {
            let _ = fs::remove_file(&part);
        }
    }

    #[cfg(test)]
    mod tests {
        use std::fs;
        use std::path::{Path, PathBuf};

        use super::{Cover, address, downloaded, sweep};

        fn scratch(name: &str) -> PathBuf {
            let folder = std::env::temp_dir().join(format!("music-{}-{name}", std::process::id()));
            let _ = fs::remove_dir_all(&folder);
            fs::create_dir_all(&folder).unwrap();
            folder
        }

        fn names(folder: &Path) -> Vec<String> {
            let mut names: Vec<String> = fs::read_dir(folder)
                .unwrap()
                .map(|entry| entry.unwrap().file_name().into_string().unwrap())
                .collect();
            names.sort();
            names
        }

        #[test]
        fn cover_file_is_named_after_the_track_and_its_version() {
            let cover = Cover::parse(
                "https://music.example.com/api/tracks/3f2a-77/cover?size=512&v=2026-11-20T09%3A37%3A00.123Z&token=SECRET.abc",
            )
            .unwrap();
            assert_eq!(cover.track, "3f2a-77");
            assert_eq!(cover.file, "3f2a-77.2026-11-20T093700123Z");
            let refreshed = Cover::parse(
                "https://music.example.com/api/tracks/3f2a-77/cover?size=512&v=2026-11-20T09%3A37%3A00.123Z&token=OTHER",
            );
            assert_eq!(refreshed, Some(cover));
            let unversioned = Cover::parse(
                "http://localhost:3001/music/api/tracks/abc/cover?size=512&token=SECRET",
            );
            assert_eq!(unversioned.unwrap().file, "abc");
        }

        #[test]
        fn only_a_track_cover_on_a_server_is_accepted() {
            for url in [
                "not an address",
                "file:///api/tracks/abc/cover",
                "music-cover://localhost/abc",
                "https://music.example.com/api/tracks/abc/stream?token=SECRET",
                "https://music.example.com/api/tracks/abc/cover/extra",
                "https://music.example.com/api/tracks//cover",
                "https://music.example.com/api/tracks/a%2Fb/cover",
                "https://music.example.com/api/tracks/../../etc/cover",
            ] {
                assert_eq!(Cover::parse(url), None, "{url}");
            }
        }

        #[test]
        fn downloaded_track_shows_its_own_cover_file() {
            let offline = scratch("offline");
            fs::write(offline.join("abc.cover"), b"image").unwrap();
            let found = downloaded(Some(offline.clone()), "abc");
            assert_eq!(found, Some(offline.join("abc.cover")));
            assert_eq!(downloaded(Some(offline.clone()), "other"), None);
            assert_eq!(downloaded(None, "abc"), None);
            fs::remove_dir_all(offline).unwrap();
        }

        #[test]
        fn shown_address_is_a_file_address() {
            assert_eq!(
                address(Path::new("/home/me/Music offline/abc.cover")).as_deref(),
                Some("file:///home/me/Music%20offline/abc.cover")
            );
        }

        #[test]
        fn sweep_leaves_only_the_cover_that_is_shown() {
            let folder = scratch("covers");
            for name in ["old", "old.2026.7.part", "now.2026", "now.2026.9.part"] {
                fs::write(folder.join(name), b"image").unwrap();
            }
            sweep(&folder, Some("now.2026"));
            assert_eq!(names(&folder), ["now.2026"]);
            sweep(&folder, None);
            assert!(names(&folder).is_empty());
            sweep(&folder.join("missing"), None);
            fs::remove_dir_all(folder).unwrap();
        }
    }
}
