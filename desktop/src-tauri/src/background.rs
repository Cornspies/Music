use std::collections::HashSet;
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crossbeam_channel::{Receiver, Sender, bounded, never, select, unbounded};
use serde::{Deserialize, Serialize};
use serde_json::json;
use tauri::{AppHandle, Manager, State, Url};
use ureq::Agent;

use crate::audio::{self, Audio, Event, Msg};
use crate::{media, window};

const FIRST_ID: u64 = 1 << 40;
const LISTEN_FLUSH_SECONDS: f64 = 300.0;
const SAVE_EVERY: Duration = Duration::from_secs(300);
const TOKEN_MARGIN_MS: f64 = 600_000.0;
const TOKEN_RETRY: Duration = Duration::from_secs(60);
const TICK: Duration = Duration::from_secs(1);
const FLUSH_WAIT: Duration = Duration::from_secs(3);
const SHUFFLE_HISTORY_MAX: usize = 500;

#[derive(Default)]
pub struct Background(Mutex<Option<Sender<Command>>>);

pub enum Command {
    Take(Sender<Taken>),
    Quit(Sender<()>),
}

type Token = Option<(String, f64)>;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Handoff {
    server: String,
    session: String,
    media_token: String,
    media_expires_at: f64,
    queue: String,
    gains: Vec<f32>,
    index: usize,
    current_id: u64,
    next_id: Option<u64>,
    next_index: Option<usize>,
    shuffle: bool,
    shuffle_all: bool,
    history: ShuffleHistory,
    repeat: String,
    sleep_ends_at: Option<f64>,
    sleep_after_track: bool,
    position: f64,
    playing: bool,
}

#[derive(Serialize, Deserialize, Default, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ShuffleHistory {
    back: Vec<String>,
    forward: Vec<String>,
    played: Vec<String>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Resume {
    queue: String,
    index: usize,
    current_id: u64,
    next_id: Option<u64>,
    next_index: Option<usize>,
    position: f64,
    duration: f64,
    playing: bool,
    sleep_ends_at: Option<f64>,
    sleep_after_track: bool,
    media_token: String,
    media_expires_at: f64,
    history: ShuffleHistory,
}

pub struct Taken {
    queue: String,
    durations: Vec<f64>,
    index: usize,
    current_id: u64,
    next: Option<(u64, usize)>,
    position: f64,
    playing: bool,
    sleep_ends_at: Option<f64>,
    sleep_after_track: bool,
    media_token: String,
    media_expires_at: f64,
    history: ShuffleHistory,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct QueueTrack {
    id: String,
    #[serde(default)]
    title: String,
    #[serde(default)]
    artist: String,
    #[serde(default)]
    album: String,
    #[serde(default)]
    duration: f64,
    #[serde(default)]
    has_cover: bool,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MediaToken {
    token: String,
    expires_at: String,
}

#[derive(PartialEq)]
enum Repeat {
    Off,
    All,
    One,
}

struct Listening {
    track: String,
    last: Option<f64>,
    unreported: f64,
}

struct Controller {
    app: AppHandle,
    engine: Sender<Msg>,
    events: Receiver<Event>,
    commands: Receiver<Command>,
    tokens: Receiver<Token>,
    tokens_tx: Sender<Token>,
    agent: Agent,
    server: String,
    session: String,
    media_token: String,
    media_expires_at: f64,
    token_attempt: Option<Instant>,
    reload_on_token: bool,
    queue_json: String,
    queue: Vec<QueueTrack>,
    gains: Vec<f32>,
    index: usize,
    current_id: u64,
    next: Option<(u64, usize)>,
    ids: u64,
    shuffle: bool,
    shuffle_all: bool,
    back: Vec<String>,
    forward: Vec<String>,
    played: HashSet<String>,
    repeat: Repeat,
    sleep_ends_at: Option<f64>,
    sleep_after_track: bool,
    position: f64,
    playing: bool,
    retried: bool,
    listening: Listening,
    dirty: bool,
    last_save: Instant,
    seed: u64,
}

impl Controller {
    fn run(mut self) {
        let mut events = self.events.clone();
        let commands = self.commands.clone();
        let tokens = self.tokens.clone();
        if self.next.is_none() {
            self.prepare_next();
        }
        loop {
            select! {
                recv(events) -> event => match event {
                    Ok(event) => self.on_event(event),
                    Err(_) => events = never(),
                },
                recv(commands) -> command => match command {
                    Ok(Command::Take(reply)) => {
                        self.dirty = true;
                        self.flush_listening();
                        self.save(true);
                        let _ = reply.send(self.taken());
                        return;
                    }
                    Ok(Command::Quit(reply)) => {
                        self.final_flush();
                        let _ = reply.send(());
                        return;
                    }
                    Err(_) => {
                        self.final_flush();
                        return;
                    }
                },
                recv(tokens) -> token => {
                    if let Ok(token) = token {
                        self.on_token(token);
                    }
                }
                default(TICK) => {}
            }
            self.tick();
        }
    }

    fn on_event(&mut self, event: Event) {
        match event {
            Event::Time { id, position } if id == self.current_id => {
                self.note_listening(position);
                if (position - self.position).abs() >= 1.0 {
                    self.dirty = true;
                }
                self.position = position;
            }
            Event::Play { id } if id == self.current_id => self.playing = true,
            Event::Pause { id } if id == self.current_id => {
                self.playing = false;
                self.flush_listening();
                self.save(true);
            }
            Event::Ended { id } if id == self.current_id => self.ended(),
            Event::Advanced { id } => {
                if let Some((next_id, index)) = self.next
                    && next_id == id
                {
                    self.note_move(index);
                    self.next = None;
                    self.playing = true;
                    self.begin(index, id);
                }
            }
            Event::Error { id, expired, .. } if id == self.current_id => {
                if expired && !self.retried {
                    self.retried = true;
                    self.reload_on_token = true;
                    self.refresh_token();
                } else {
                    self.playing = false;
                }
            }
            Event::Media { action, position } => self.media(&action, position),
            _ => {}
        }
    }

    fn media(&mut self, action: &str, position: Option<f64>) {
        match action {
            "play" => self.send(Msg::Play),
            "pause" => self.send(Msg::Pause),
            "toggle" => self.send(if self.playing { Msg::Pause } else { Msg::Play }),
            "next" => self.next(),
            "previous" => self.previous(),
            "seekBy" => {
                if let Some(delta) = position {
                    self.seek((self.position + delta).max(0.0));
                }
            }
            "seekTo" => {
                if let Some(to) = position {
                    self.seek(to);
                }
            }
            _ => {}
        }
    }

    fn tick(&mut self) {
        if let Some(at) = self.sleep_ends_at
            && now_ms() >= at
        {
            self.sleep_ends_at = None;
            if self.playing {
                self.send(Msg::Pause);
            }
        }
        if self.media_expires_at - now_ms() < TOKEN_MARGIN_MS {
            self.refresh_token();
        }
        if self.playing {
            self.save(false);
        }
    }

    fn send(&self, msg: Msg) {
        let _ = self.engine.send(msg);
    }

    fn seek(&mut self, to: f64) {
        let duration = self.queue.get(self.index).map_or(0.0, |t| t.duration);
        let to = if duration > 0.0 { to.min(duration) } else { to };
        self.position = to;
        self.dirty = true;
        self.send(Msg::Seek(to));
    }

    fn ended(&mut self) {
        self.playing = false;
        if self.sleep_after_track {
            self.sleep_after_track = false;
            self.flush_listening();
            self.save(true);
            return;
        }
        if self.repeat == Repeat::One {
            self.send(Msg::Seek(0.0));
            self.send(Msg::Play);
            self.report_play();
            return;
        }
        self.next();
    }

    fn next(&mut self) {
        let count = self.queue.len();
        if count == 0 {
            return;
        }
        if count == 1 {
            self.send(Msg::Seek(0.0));
            if self.repeat != Repeat::Off || self.playing {
                self.send(Msg::Play);
                self.report_play();
            }
            return;
        }
        match self
            .next
            .map(|(_, index)| index)
            .or_else(|| self.pick(false))
        {
            Some(index) => {
                self.note_move(index);
                self.play_index(index)
            }
            None => {
                self.send(Msg::Pause);
                self.send(Msg::Seek(0.0));
                self.playing = false;
                self.position = 0.0;
            }
        }
    }

    fn previous(&mut self) {
        if self.position > 3.0 {
            self.seek(0.0);
            return;
        }
        if self.shuffle {
            while let Some(id) = self.back.pop() {
                if let Some(index) = self.queue.iter().position(|t| t.id == id)
                    && index != self.index
                {
                    let current = self.queue[self.index].id.clone();
                    self.forward.push(current);
                    self.play_index(index);
                    return;
                }
            }
            self.seek(0.0);
            return;
        }
        let target = if self.index > 0 {
            Some(self.index - 1)
        } else if self.repeat == Repeat::All && !self.queue.is_empty() {
            Some(self.queue.len() - 1)
        } else {
            None
        };
        match target {
            Some(index) => self.play_index(index),
            None => self.seek(0.0),
        }
    }

    fn pick(&mut self, automatic: bool) -> Option<usize> {
        let count = self.queue.len();
        if count < 2 || (automatic && (self.sleep_after_track || self.repeat == Repeat::One)) {
            return None;
        }
        if self.shuffle {
            if let Some(index) = self.forward_index() {
                return Some(index);
            }
            if self.shuffle_all {
                return self.pick_fresh();
            }
            loop {
                let index = self.random(count);
                if index != self.index {
                    return Some(index);
                }
            }
        }
        if self.index + 1 < count {
            Some(self.index + 1)
        } else if self.repeat == Repeat::All {
            Some(0)
        } else {
            None
        }
    }

    fn forward_index(&mut self) -> Option<usize> {
        while let Some(id) = self.forward.last().cloned() {
            if let Some(index) = self.queue.iter().position(|t| t.id == id)
                && index != self.index
            {
                return Some(index);
            }
            self.forward.pop();
        }
        None
    }

    fn pick_fresh(&mut self) -> Option<usize> {
        let count = self.queue.len();
        let mut fresh: Vec<usize> = (0..count)
            .filter(|&i| i != self.index && !self.played.contains(&self.queue[i].id))
            .collect();
        if fresh.is_empty() {
            if self.repeat != Repeat::All {
                return None;
            }
            let current = self.queue[self.index].id.clone();
            self.played.clear();
            self.played.insert(current.clone());
            fresh = (0..count)
                .filter(|&i| i != self.index && self.queue[i].id != current)
                .collect();
            if fresh.is_empty() {
                return None;
            }
        }
        let pick = self.random(fresh.len());
        Some(fresh[pick])
    }

    fn note_move(&mut self, to: usize) {
        if !self.shuffle || to >= self.queue.len() || self.index >= self.queue.len() {
            return;
        }
        let from = self.queue[self.index].id.clone();
        let to_id = self.queue[to].id.clone();
        if from == to_id {
            return;
        }
        self.back.push(from);
        if self.back.len() > SHUFFLE_HISTORY_MAX {
            self.back.remove(0);
        }
        if self.forward.last() == Some(&to_id) {
            self.forward.pop();
        } else {
            self.forward.clear();
        }
    }

    fn random(&mut self, below: usize) -> usize {
        self.seed ^= self.seed << 13;
        self.seed ^= self.seed >> 7;
        self.seed ^= self.seed << 17;
        (self.seed % below as u64) as usize
    }

    fn new_id(&mut self) -> u64 {
        self.ids += 1;
        self.ids
    }

    fn play_index(&mut self, index: usize) {
        if index >= self.queue.len() {
            return;
        }
        let id = match self.next.take() {
            Some((id, prepared)) if prepared == index => id,
            _ => self.new_id(),
        };
        self.send(Msg::Load {
            id,
            url: self.stream_url(index),
            start: 0.0,
            autoplay: true,
            gain: self.gains[index],
        });
        self.playing = true;
        self.begin(index, id);
    }

    fn begin(&mut self, index: usize, id: u64) {
        self.flush_listening();
        self.index = index;
        if self.shuffle && self.shuffle_all {
            self.played.insert(self.queue[index].id.clone());
        }
        self.current_id = id;
        self.position = 0.0;
        self.retried = false;
        self.listening = Listening {
            track: self.queue[index].id.clone(),
            last: None,
            unreported: 0.0,
        };
        self.report_play();
        self.show_metadata();
        self.dirty = true;
        self.save(true);
        self.prepare_next();
    }

    fn prepare_next(&mut self) {
        let next = self.pick(true).map(|index| (self.new_id(), index));
        self.next = next;
        let prepared = next.map(|(id, index)| (id, self.stream_url(index), self.gains[index]));
        self.send(Msg::Prepare { next: prepared });
    }

    fn reload(&mut self) {
        let id = self.new_id();
        self.next = None;
        self.send(Msg::Load {
            id,
            url: self.stream_url(self.index),
            start: self.position,
            autoplay: true,
            gain: self.gains[self.index],
        });
        self.current_id = id;
        self.playing = true;
        self.prepare_next();
    }

    fn url(&self, path: &str, extra: &[(&str, &str)]) -> String {
        let Ok(mut url) = Url::parse(&format!("{}{path}", self.server)) else {
            return String::new();
        };
        {
            let mut query = url.query_pairs_mut();
            for (key, value) in extra {
                query.append_pair(key, value);
            }
            query.append_pair("token", &self.media_token);
        }
        url.into()
    }

    fn stream_url(&self, index: usize) -> String {
        let track = &self.queue[index];
        self.url(
            &format!("/api/tracks/{}/stream", track.id),
            &[("version", "compatible")],
        )
    }

    fn show_metadata(&self) {
        let track = &self.queue[self.index];
        let cover = track.has_cover.then(|| {
            self.url(
                &format!("/api/tracks/{}/cover", track.id),
                &[("size", "512")],
            )
        });
        let artist = if track.artist.is_empty() {
            "Unknown artist".to_string()
        } else {
            track.artist.clone()
        };
        let album = (!track.album.is_empty()).then(|| track.album.clone());
        media::metadata(
            &self.app,
            track.title.clone(),
            Some(artist),
            album,
            cover,
            Some(track.duration),
        );
    }

    fn note_listening(&mut self, position: f64) {
        if let Some(last) = self.listening.last {
            let delta = position - last;
            if delta > 0.0 && delta < 2.0 {
                self.listening.unreported += delta;
            }
        }
        self.listening.last = Some(position);
        if self.listening.unreported >= LISTEN_FLUSH_SECONDS {
            self.flush_listening();
        }
    }

    fn flush_listening(&mut self) -> Option<Receiver<()>> {
        if self.listening.unreported < 0.5 {
            return None;
        }
        let seconds = (self.listening.unreported * 10.0).round() / 10.0;
        self.listening.unreported = 0.0;
        let body = json!({ "seconds": seconds }).to_string();
        Some(self.call(
            "POST",
            format!("/api/tracks/{}/listened", self.listening.track),
            Some(body),
        ))
    }

    fn report_play(&self) {
        let track = &self.queue[self.index];
        self.call("POST", format!("/api/tracks/{}/play", track.id), None);
    }

    fn save(&mut self, force: bool) -> Option<Receiver<()>> {
        if !self.dirty || (!force && self.last_save.elapsed() < SAVE_EVERY) {
            return None;
        }
        self.dirty = false;
        self.last_save = Instant::now();
        let body = json!({
            "trackId": self.queue[self.index].id,
            "position": self.position,
        })
        .to_string();
        Some(self.call("PUT", "/api/playback".into(), Some(body)))
    }

    fn final_flush(&mut self) {
        self.dirty = true;
        let pending: Vec<Receiver<()>> = [self.flush_listening(), self.save(true)]
            .into_iter()
            .flatten()
            .collect();
        let deadline = Instant::now() + FLUSH_WAIT;
        for done in pending {
            let _ = done.recv_timeout(deadline.saturating_duration_since(Instant::now()));
        }
    }

    fn call(&self, method: &'static str, path: String, body: Option<String>) -> Receiver<()> {
        let (done, finished) = bounded(1);
        let agent = self.agent.clone();
        let url = format!("{}{path}", self.server);
        let auth = format!("Bearer {}", self.session);
        thread::spawn(move || {
            let _ = request(&agent, method, &url, &auth, body.as_deref());
            let _ = done.send(());
        });
        finished
    }

    fn refresh_token(&mut self) {
        if self
            .token_attempt
            .is_some_and(|at| at.elapsed() < TOKEN_RETRY)
        {
            return;
        }
        self.token_attempt = Some(Instant::now());
        let agent = self.agent.clone();
        let url = format!("{}/api/auth/media-token", self.server);
        let auth = format!("Bearer {}", self.session);
        let reply = self.tokens_tx.clone();
        thread::spawn(move || {
            let _ = reply.send(fetch_token(&agent, &url, &auth));
        });
    }

    fn on_token(&mut self, token: Token) {
        if let Some((token, expires_at)) = token {
            self.media_token = token;
            self.media_expires_at = expires_at;
            self.token_attempt = None;
            if self.reload_on_token {
                self.reload_on_token = false;
                self.reload();
            }
        } else if self.reload_on_token {
            self.reload_on_token = false;
            self.playing = false;
        }
    }

    fn taken(&self) -> Taken {
        Taken {
            queue: self.queue_json.clone(),
            durations: self.queue.iter().map(|t| t.duration).collect(),
            index: self.index,
            current_id: self.current_id,
            next: self.next,
            position: self.position,
            playing: self.playing,
            sleep_ends_at: self.sleep_ends_at,
            sleep_after_track: self.sleep_after_track,
            media_token: self.media_token.clone(),
            media_expires_at: self.media_expires_at,
            history: ShuffleHistory {
                back: self.back.clone(),
                forward: self.forward.clone(),
                played: self.played.iter().cloned().collect(),
            },
        }
    }
}

fn request(
    agent: &Agent,
    method: &str,
    url: &str,
    auth: &str,
    body: Option<&str>,
) -> Option<String> {
    let response = match method {
        "GET" => agent.get(url).header("Authorization", auth).call(),
        "PUT" => agent
            .put(url)
            .header("Authorization", auth)
            .header("Content-Type", "application/json")
            .send(body.unwrap_or("{}")),
        _ => agent
            .post(url)
            .header("Authorization", auth)
            .header("Content-Type", "application/json")
            .send(body.unwrap_or("")),
    };
    let mut response = response.ok()?;
    if !response.status().is_success() {
        return None;
    }
    response.body_mut().read_to_string().ok()
}

fn fetch_token(agent: &Agent, url: &str, auth: &str) -> Option<(String, f64)> {
    let body = request(agent, "GET", url, auth, None)?;
    let token: MediaToken = serde_json::from_str(&body).ok()?;
    Some((token.token, parse_utc(&token.expires_at)?))
}

fn now_ms() -> f64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs_f64() * 1000.0)
        .unwrap_or(0.0)
}

fn parse_utc(stamp: &str) -> Option<f64> {
    let field = |from: usize, to: usize| stamp.get(from..to)?.parse::<i64>().ok();
    let days = days_from_civil(field(0, 4)?, field(5, 7)?, field(8, 10)?);
    let seconds = days * 86_400 + field(11, 13)? * 3_600 + field(14, 16)? * 60 + field(17, 19)?;
    Some(seconds as f64 * 1000.0)
}

fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let year = if month <= 2 { year - 1 } else { year };
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

fn repeat_mode(mode: &str) -> Repeat {
    match mode {
        "all" => Repeat::All,
        "one" => Repeat::One,
        _ => Repeat::Off,
    }
}

#[tauri::command]
pub fn background_start(
    app: AppHandle,
    audio: State<'_, Audio>,
    background: State<'_, Background>,
    handoff: Handoff,
) -> Result<(), String> {
    let queue: Vec<QueueTrack> = serde_json::from_str(&handoff.queue).map_err(|e| e.to_string())?;
    if handoff.index >= queue.len() || handoff.gains.len() != queue.len() {
        return Err("the queue and its gains don't match".into());
    }
    let (events_tx, events) = unbounded();
    let (commands_tx, commands) = unbounded();
    let (tokens_tx, tokens) = unbounded();
    let next = handoff
        .next_id
        .zip(handoff.next_index)
        .filter(|(_, index)| *index < queue.len());
    let seed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(1)
        | 1;
    let controller = Controller {
        app: app.clone(),
        engine: audio.sender(),
        events,
        commands,
        tokens,
        tokens_tx,
        agent: audio::http_agent(),
        server: handoff.server.trim_end_matches('/').to_string(),
        session: handoff.session,
        media_token: handoff.media_token,
        media_expires_at: handoff.media_expires_at,
        token_attempt: None,
        reload_on_token: false,
        listening: Listening {
            track: queue[handoff.index].id.clone(),
            last: None,
            unreported: 0.0,
        },
        queue_json: handoff.queue,
        queue,
        gains: handoff.gains,
        index: handoff.index,
        current_id: handoff.current_id,
        next,
        ids: FIRST_ID
            .max(handoff.current_id)
            .max(handoff.next_id.unwrap_or(0)),
        shuffle: handoff.shuffle,
        shuffle_all: handoff.shuffle_all,
        back: handoff.history.back,
        forward: handoff.history.forward,
        played: handoff.history.played.into_iter().collect(),
        repeat: repeat_mode(&handoff.repeat),
        sleep_ends_at: handoff.sleep_ends_at,
        sleep_after_track: handoff.sleep_after_track,
        position: handoff.position,
        playing: handoff.playing,
        retried: false,
        dirty: false,
        last_save: Instant::now(),
        seed,
    };
    if let Ok(mut sink) = audio.sink().lock() {
        sink.set_background(Some(events_tx));
    }
    if let Err(err) = thread::Builder::new()
        .name("background".into())
        .spawn(move || controller.run())
    {
        if let Ok(mut sink) = audio.sink().lock() {
            sink.set_background(None);
        }
        return Err(err.to_string());
    }
    if let Ok(mut slot) = background.0.lock() {
        *slot = Some(commands_tx);
    }
    window::enter_background(&app);
    Ok(())
}

#[tauri::command(async)]
pub fn background_take(
    audio: State<'_, Audio>,
    background: State<'_, Background>,
) -> Result<Option<Resume>, String> {
    Ok(take(&audio, &background))
}

fn take(audio: &Audio, background: &Background) -> Option<Resume> {
    let commands = background.0.lock().ok()?.take()?;
    let (reply, answer) = bounded(1);
    let sent = commands.send(Command::Take(reply)).is_ok();
    let taken = answer.recv_timeout(FLUSH_WAIT).ok().filter(|_| sent);
    if let Ok(mut sink) = audio.sink().lock() {
        sink.set_background(None);
    }
    let taken = taken?;
    let snapshot = audio.snapshot();
    let engine_current = snapshot.as_ref().and_then(|s| s.current);
    let engine_next = snapshot.as_ref().and_then(|s| s.next);
    let (index, current_id, next) = match taken.next {
        Some((id, index)) if engine_current == Some(id) => (index, id, None),
        next => (taken.index, taken.current_id, next),
    };
    let next = next.filter(|(id, _)| engine_next == Some(*id));
    Some(Resume {
        queue: taken.queue,
        index,
        current_id,
        next_id: next.map(|(id, _)| id),
        next_index: next.map(|(_, index)| index),
        position: snapshot.as_ref().map_or(taken.position, |s| s.position),
        duration: snapshot
            .as_ref()
            .and_then(|s| s.duration)
            .unwrap_or_else(|| taken.durations.get(index).copied().unwrap_or(0.0)),
        playing: snapshot.as_ref().map_or(taken.playing, |s| s.playing),
        sleep_ends_at: taken.sleep_ends_at,
        sleep_after_track: taken.sleep_after_track,
        media_token: taken.media_token,
        media_expires_at: taken.media_expires_at,
        history: taken.history,
    })
}

pub fn stop(app: &AppHandle) {
    let Some(commands) = app
        .state::<Background>()
        .0
        .lock()
        .ok()
        .and_then(|mut slot| slot.take())
    else {
        return;
    };
    let (reply, answer) = bounded(1);
    if commands.send(Command::Quit(reply)).is_ok() {
        let _ = answer.recv_timeout(FLUSH_WAIT + Duration::from_secs(1));
    }
    if let Ok(mut sink) = app.state::<Audio>().sink().lock() {
        sink.set_background(None);
    }
}
