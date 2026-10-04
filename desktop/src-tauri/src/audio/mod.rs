mod decode;
mod engine;
mod http;
mod output;

use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use crossbeam_channel::{Sender, bounded, unbounded};
use serde::Serialize;
use tauri::ipc::Channel;
use tauri::{AppHandle, State};

#[derive(Default)]
pub struct Sink {
    js: Option<Channel<Event>>,
    background: Option<Sender<Event>>,
}

impl Sink {
    pub fn send(&self, event: Event) {
        if let Some(background) = self.background.as_ref() {
            let _ = background.send(event);
        } else if let Some(channel) = self.js.as_ref() {
            let _ = channel.send(event);
        }
    }

    pub fn set_background(&mut self, background: Option<Sender<Event>>) {
        self.background = background;
    }
}

pub type Emitter = Arc<Mutex<Sink>>;

pub fn http_agent() -> ureq::Agent {
    http::agent()
}

pub enum Msg {
    Load {
        id: u64,
        url: String,
        start: f64,
        autoplay: bool,
        gain: f32,
    },
    Prepare {
        next: Option<(u64, String, f32)>,
    },
    Play,
    Pause,
    Seek(f64),
    Volume {
        volume: f32,
        muted: bool,
    },
    Gains {
        current: f32,
        next: f32,
    },
    Crossfade(f64),
    Stop,
    Info(Sender<Option<Info>>),
    Snapshot(Sender<Snapshot>),
    Media(String, Option<f64>),
    PreviewPlay {
        url: String,
        start: f64,
        until: f64,
    },
    PreviewStop,
    PreviewEnded(u64),
    Advanced(u64),
    Ended(u64),
    Failed {
        id: u64,
        message: String,
        expired: bool,
    },
    DeviceError(String),
}

#[derive(Clone, Serialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum Event {
    Time {
        id: u64,
        position: f64,
    },
    Duration {
        id: u64,
        duration: f64,
    },
    Play {
        id: u64,
    },
    Pause {
        id: u64,
    },
    Ended {
        id: u64,
    },
    Advanced {
        id: u64,
    },
    Error {
        id: u64,
        message: String,
        expired: bool,
    },
    Media {
        action: String,
        position: Option<f64>,
    },
    Preview {
        state: String,
    },
}

pub struct Snapshot {
    pub current: Option<u64>,
    pub next: Option<u64>,
    pub position: f64,
    pub duration: Option<f64>,
    pub playing: bool,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Info {
    pub device: String,
    pub sample_rate: u32,
    pub channels: u16,
    pub format: String,
    pub playing: bool,
}

pub struct Audio {
    tx: Sender<Msg>,
    events: Emitter,
}

impl Audio {
    pub fn start(app: AppHandle) -> Self {
        let (tx, rx) = unbounded();
        let events: Emitter = Arc::new(Mutex::new(Sink::default()));
        let engine = engine::Engine::new(rx, tx.clone(), events.clone(), app);
        let _ = thread::Builder::new()
            .name("audio".into())
            .spawn(move || engine.run());
        Audio { tx, events }
    }

    pub fn sender(&self) -> Sender<Msg> {
        self.tx.clone()
    }

    pub fn sink(&self) -> Emitter {
        self.events.clone()
    }

    pub fn snapshot(&self) -> Option<Snapshot> {
        let (reply, answer) = bounded(1);
        self.send(Msg::Snapshot(reply));
        answer.recv_timeout(Duration::from_secs(2)).ok()
    }

    fn send(&self, msg: Msg) {
        let _ = self.tx.send(msg);
    }
}

#[tauri::command]
pub fn audio_init(audio: State<'_, Audio>, events: Channel<Event>) {
    if let Ok(mut sink) = audio.events.lock() {
        sink.js = Some(events);
    }
}

#[tauri::command]
pub fn audio_load(
    audio: State<'_, Audio>,
    id: u64,
    url: String,
    start_at: f64,
    autoplay: bool,
    gain: f32,
) {
    audio.send(Msg::Load {
        id,
        url,
        start: start_at,
        autoplay,
        gain,
    });
}

#[tauri::command]
pub fn audio_prepare(audio: State<'_, Audio>, id: Option<u64>, url: Option<String>, gain: f32) {
    let next = match (id, url) {
        (Some(id), Some(url)) => Some((id, url, gain)),
        _ => None,
    };
    audio.send(Msg::Prepare { next });
}

#[tauri::command]
pub fn audio_play(audio: State<'_, Audio>) {
    audio.send(Msg::Play);
}

#[tauri::command]
pub fn audio_pause(audio: State<'_, Audio>) {
    audio.send(Msg::Pause);
}

#[tauri::command]
pub fn audio_seek(audio: State<'_, Audio>, position: f64) {
    audio.send(Msg::Seek(position));
}

#[tauri::command]
pub fn audio_volume(audio: State<'_, Audio>, volume: f32, muted: bool) {
    audio.send(Msg::Volume { volume, muted });
}

#[tauri::command]
pub fn audio_gains(audio: State<'_, Audio>, current: f32, next: f32) {
    audio.send(Msg::Gains { current, next });
}

#[tauri::command]
pub fn audio_crossfade(audio: State<'_, Audio>, seconds: f64) {
    audio.send(Msg::Crossfade(seconds));
}

#[tauri::command]
pub fn audio_stop(audio: State<'_, Audio>) {
    audio.send(Msg::Stop);
}

#[tauri::command]
pub fn audio_preview(audio: State<'_, Audio>, url: String, start: f64, until: f64) {
    audio.send(Msg::PreviewPlay { url, start, until });
}

#[tauri::command]
pub fn audio_preview_stop(audio: State<'_, Audio>) {
    audio.send(Msg::PreviewStop);
}

#[tauri::command(async)]
pub fn audio_info(audio: State<'_, Audio>) -> Result<Option<Info>, String> {
    let (reply, answer) = bounded(1);
    audio.send(Msg::Info(reply));
    Ok(answer.recv_timeout(Duration::from_secs(2)).ok().flatten())
}
