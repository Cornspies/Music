use std::sync::OnceLock;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crossbeam_channel::{Receiver, RecvTimeoutError, Sender, unbounded};
use discord_rich_presence::activity::{
    Activity, ActivityType, Assets, StatusDisplayType, Timestamps,
};
use discord_rich_presence::{DiscordIpc, DiscordIpcClient};

const CLIENT_ID: &str = "1557743084828237905";
const LARGE_IMAGE: &str = "music";
const MIN_GAP: Duration = Duration::from_secs(4);
const RETRY: Duration = Duration::from_secs(15);
const IDLE: Duration = Duration::from_secs(3600);
const DRIFT_SECONDS: i64 = 3;

static UPDATES: OnceLock<Sender<Update>> = OnceLock::new();
static CONNECTED: AtomicBool = AtomicBool::new(false);

enum Update {
    Enabled(bool),
    Track(Track),
    Playback(Option<(bool, f64)>),
}

#[derive(Clone, PartialEq)]
struct Track {
    title: String,
    artist: Option<String>,
    album: Option<String>,
    duration: Option<f64>,
}

#[derive(PartialEq)]
struct Shown {
    title: String,
    start: i64,
}

pub fn available() -> bool {
    !CLIENT_ID.is_empty()
}

pub fn connected() -> bool {
    CONNECTED.load(Ordering::Relaxed)
}

pub fn start(enabled: bool) {
    if !available() {
        return;
    }
    let (tx, rx) = unbounded();
    if UPDATES.set(tx).is_err() {
        return;
    }
    let _ = thread::Builder::new()
        .name("discord".into())
        .spawn(move || Presence::new(enabled).run(&rx));
}

fn send(update: Update) {
    if let Some(updates) = UPDATES.get() {
        let _ = updates.send(update);
    }
}

pub fn set_enabled(on: bool) {
    send(Update::Enabled(on));
}

pub fn track(title: &str, artist: Option<&str>, album: Option<&str>, duration: Option<f64>) {
    send(Update::Track(Track {
        title: title.to_string(),
        artist: artist.map(str::to_string),
        album: album.map(str::to_string),
        duration,
    }));
}

pub fn playback(state: Option<(bool, f64)>) {
    send(Update::Playback(state));
}

struct Presence {
    enabled: bool,
    track: Option<Track>,
    playing: bool,
    position: f64,
    position_at: Instant,
    client: Option<DiscordIpcClient>,
    shown: Option<Shown>,
    dirty: bool,
    last_push: Option<Instant>,
    last_attempt: Option<Instant>,
}

fn now_seconds() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn field(text: &str) -> String {
    let mut out: String = text.trim().chars().take(128).collect();
    while out.chars().count() < 2 {
        out.push('\u{2800}');
    }
    out
}

impl Presence {
    fn new(enabled: bool) -> Self {
        Presence {
            enabled,
            track: None,
            playing: false,
            position: 0.0,
            position_at: Instant::now(),
            client: None,
            shown: None,
            dirty: true,
            last_push: None,
            last_attempt: None,
        }
    }

    fn run(mut self, updates: &Receiver<Update>) {
        loop {
            match updates.recv_timeout(self.wait()) {
                Ok(update) => {
                    self.apply(update);
                    continue;
                }
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => {
                    self.disconnect();
                    return;
                }
            }
            self.push();
        }
    }

    fn wait(&self) -> Duration {
        if !self.dirty {
            return IDLE;
        }
        let since = |at: Option<Instant>| at.map_or(MIN_GAP, |at| at.elapsed());
        let gap = if self.client.is_none() && self.last_attempt.is_some() {
            RETRY.saturating_sub(since(self.last_attempt))
        } else {
            MIN_GAP.saturating_sub(since(self.last_push))
        };
        gap.max(Duration::from_millis(250))
    }

    fn apply(&mut self, update: Update) {
        match update {
            Update::Enabled(on) => {
                self.enabled = on;
                self.last_attempt = None;
            }
            Update::Track(track) => self.track = Some(track),
            Update::Playback(None) => {
                self.track = None;
                self.playing = false;
            }
            Update::Playback(Some((playing, position))) => {
                self.playing = playing;
                self.position = position;
                self.position_at = Instant::now();
            }
        }
        self.dirty = self.wanted() != self.shown;
    }

    fn wanted(&self) -> Option<Shown> {
        let track = self
            .track
            .as_ref()
            .filter(|_| self.enabled && self.playing)?;
        let elapsed = self.position + self.position_at.elapsed().as_secs_f64();
        Some(Shown {
            title: track.title.clone(),
            start: now_seconds() - elapsed.round() as i64,
        })
    }

    fn push(&mut self) {
        let wanted = self.wanted();
        let unchanged = match (&wanted, &self.shown) {
            (Some(w), Some(s)) => w.title == s.title && (w.start - s.start).abs() <= DRIFT_SECONDS,
            (None, None) => true,
            _ => false,
        };
        if unchanged {
            self.dirty = false;
            if !self.enabled {
                self.disconnect();
            }
            return;
        }
        let Some(wanted) = wanted else {
            if let Some(client) = self.client.as_mut() {
                let _ = client.clear_activity();
            }
            self.shown = None;
            self.last_push = Some(Instant::now());
            self.dirty = false;
            if !self.enabled {
                self.disconnect();
            }
            return;
        };
        if !self.connect() {
            return;
        }
        let Some(track) = self.track.clone() else {
            return;
        };
        let artist = track
            .artist
            .as_deref()
            .filter(|a| !a.trim().is_empty())
            .unwrap_or("Unknown artist");
        let details = field(&track.title);
        let state = field(artist);
        let album = track
            .album
            .as_deref()
            .filter(|a| !a.trim().is_empty())
            .map(field);
        let mut assets = Assets::new().large_image(LARGE_IMAGE);
        if let Some(album) = album.as_deref() {
            assets = assets.large_text(album);
        }
        let mut timestamps = Timestamps::new().start(wanted.start * 1000);
        if let Some(duration) = track.duration.filter(|d| d.is_finite() && *d > 0.0) {
            timestamps = timestamps.end((wanted.start as f64 * 1000.0 + duration * 1000.0) as i64);
        }
        let activity = Activity::new()
            .activity_type(ActivityType::Listening)
            .status_display_type(StatusDisplayType::Details)
            .details(&details)
            .state(&state)
            .assets(assets)
            .timestamps(timestamps);
        let sent = self
            .client
            .as_mut()
            .is_some_and(|client| client.set_activity(activity).is_ok());
        self.last_push = Some(Instant::now());
        if sent {
            self.shown = Some(wanted);
            self.dirty = false;
        } else {
            self.disconnect();
            self.last_attempt = Some(Instant::now());
        }
    }

    fn connect(&mut self) -> bool {
        if self.client.is_some() {
            return true;
        }
        if self.last_attempt.is_some_and(|at| at.elapsed() < RETRY) {
            return false;
        }
        self.last_attempt = Some(Instant::now());
        let mut client = DiscordIpcClient::new(CLIENT_ID);
        if client.connect().is_err() {
            return false;
        }
        self.client = Some(client);
        self.shown = None;
        self.last_attempt = None;
        CONNECTED.store(true, Ordering::Relaxed);
        true
    }

    fn disconnect(&mut self) {
        if let Some(mut client) = self.client.take() {
            let _ = client.clear_activity();
            let _ = client.close();
        }
        self.shown = None;
        CONNECTED.store(false, Ordering::Relaxed);
    }
}
