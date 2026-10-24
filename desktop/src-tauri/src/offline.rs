use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};

use serde::Serialize;
use tauri::http::{Request, Response, StatusCode, header};
use tauri::ipc::Channel;
use tauri::{AppHandle, Manager, State};
use ureq::Agent;

const PROGRESS_STEP: u64 = 256 * 1024;
const FOLDER_NAME: &str = "Music offline";
const OWN_EXTENSIONS: [&str; 6] = ["json", "audio", "part", "type", "cover", "covertype"];

static DIR: RwLock<Option<PathBuf>> = RwLock::new(None);

#[derive(Default)]
pub struct Downloads(Mutex<HashMap<String, Arc<AtomicBool>>>);

#[derive(Clone, Serialize)]
pub struct Progress {
    loaded: u64,
    total: u64,
}

pub fn setup(app: &AppHandle, configured: Option<PathBuf>) {
    if let Ok(mut dir) = DIR.write() {
        *dir = configured.or_else(|| default_dir(app));
    }
}

pub fn default_dir(app: &AppHandle) -> Option<PathBuf> {
    app.path()
        .app_data_dir()
        .ok()
        .map(|dir| dir.join("offline"))
}

pub fn dir() -> Option<PathBuf> {
    DIR.read().ok().and_then(|dir| dir.clone())
}

pub fn folder_inside(picked: &Path) -> PathBuf {
    if picked.file_name().is_some_and(|name| name == FOLDER_NAME) {
        picked.to_path_buf()
    } else {
        picked.join(FOLDER_NAME)
    }
}

fn own_files(dir: &Path) -> Vec<PathBuf> {
    let Ok(entries) = fs::read_dir(dir) else {
        return Vec::new();
    };
    entries
        .flatten()
        .map(|entry| entry.path())
        .filter(|file| {
            file.is_file()
                && file.file_stem().and_then(|s| s.to_str()).is_some_and(valid)
                && file
                    .extension()
                    .and_then(|e| e.to_str())
                    .is_some_and(|e| OWN_EXTENSIONS.contains(&e))
        })
        .collect()
}

pub fn move_to(downloads: &Downloads, target: &Path) -> Result<(), String> {
    let active = downloads.0.lock().map_err(|e| e.to_string())?;
    if !active.is_empty() {
        return Err("Wait until the songs that are downloading have finished.".into());
    }
    let Some(from) = dir() else {
        return Err("There's no downloads folder.".into());
    };
    if from == target {
        return Ok(());
    }
    fs::create_dir_all(target).map_err(|e| format!("Couldn't create {}: {e}", target.display()))?;
    let files = own_files(&from);
    let mut moved: Vec<(&PathBuf, PathBuf, bool)> = Vec::new();
    for file in &files {
        let Some(name) = file.file_name() else {
            continue;
        };
        let to = target.join(name);
        if fs::rename(file, &to).is_ok() {
            moved.push((file, to, true));
            continue;
        }
        if let Err(e) = fs::copy(file, &to) {
            let _ = fs::remove_file(&to);
            for (back, to, renamed) in moved {
                if renamed {
                    let _ = fs::rename(&to, back);
                } else {
                    let _ = fs::remove_file(&to);
                }
            }
            return Err(format!("Couldn't move the downloads: {e}"));
        }
        moved.push((file, to, false));
    }
    if let Ok(mut dir) = DIR.write() {
        *dir = Some(target.to_path_buf());
    }
    drop(active);
    for file in &files {
        let _ = fs::remove_file(file);
    }
    let _ = fs::remove_dir(&from);
    Ok(())
}

fn valid(id: &str) -> bool {
    !id.is_empty() && id.len() <= 64 && id.chars().all(|c| c.is_ascii_alphanumeric() || c == '-')
}

fn path(id: &str, ext: &str) -> Option<PathBuf> {
    if !valid(id) {
        return None;
    }
    dir().map(|dir| dir.join(format!("{id}.{ext}")))
}

pub fn local_audio(url: &str) -> Option<(File, Option<String>)> {
    let rest = url.split_once("/api/tracks/")?.1;
    let (id, tail) = rest.split_once('/')?;
    if !tail.starts_with("stream") {
        return None;
    }
    let file = File::open(path(id, "audio")?).ok()?;
    let content_type = path(id, "type").and_then(|p| fs::read_to_string(p).ok());
    Some((file, content_type))
}

fn fetch(
    agent: &Agent,
    url: &str,
    auth: &str,
    to: &Path,
    cancel: &AtomicBool,
    progress: Option<&Channel<Progress>>,
) -> Result<Option<String>, String> {
    let mut response = agent
        .get(url)
        .header("Authorization", auth)
        .call()
        .map_err(|e| e.to_string())?;
    let status = response.status().as_u16();
    if !(200..300).contains(&status) {
        return Err(format!("Download failed ({status})"));
    }
    let content_type = response
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .map(String::from);
    let total = response.body().content_length().unwrap_or(0);
    let part = to.with_extension("part");
    let mut out = File::create(&part).map_err(|e| e.to_string())?;
    let mut reader = response.body_mut().as_reader();
    let mut buffer = vec![0u8; 64 * 1024];
    let mut loaded = 0u64;
    let mut reported = 0u64;
    loop {
        if cancel.load(Ordering::Relaxed) {
            drop(out);
            let _ = fs::remove_file(&part);
            return Err("cancelled".into());
        }
        let read = reader.read(&mut buffer).map_err(|e| e.to_string())?;
        if read == 0 {
            break;
        }
        out.write_all(&buffer[..read]).map_err(|e| e.to_string())?;
        loaded += read as u64;
        if let Some(progress) = progress
            && loaded - reported >= PROGRESS_STEP
        {
            reported = loaded;
            let _ = progress.send(Progress { loaded, total });
        }
    }
    if let Some(progress) = progress {
        let _ = progress.send(Progress { loaded, total });
    }
    if total > 0 && loaded != total {
        drop(out);
        let _ = fs::remove_file(&part);
        return Err(format!(
            "The download stopped early ({loaded} of {total} bytes)."
        ));
    }
    out.sync_all().map_err(|e| e.to_string())?;
    drop(out);
    fs::rename(&part, to).map_err(|e| e.to_string())?;
    Ok(content_type)
}

fn download(
    id: &str,
    url: &str,
    cover_url: Option<&str>,
    auth: &str,
    meta: &str,
    progress: &Channel<Progress>,
    cancel: &AtomicBool,
) -> Result<(), String> {
    let dir = dir().ok_or("there's no folder for downloads")?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let audio = path(id, "audio").ok_or("invalid track")?;
    let agent = crate::audio::http_agent();
    if let Some(content_type) = fetch(&agent, url, auth, &audio, cancel, Some(progress))? {
        let _ = fs::write(path(id, "type").ok_or("invalid track")?, content_type);
    }
    if let Some(cover_url) = cover_url
        && let Some(cover) = path(id, "cover")
        && let Ok(Some(content_type)) = fetch(&agent, cover_url, auth, &cover, cancel, None)
        && let Some(cover_type) = path(id, "covertype")
    {
        let _ = fs::write(cover_type, content_type);
    }
    fs::write(path(id, "json").ok_or("invalid track")?, meta).map_err(|e| e.to_string())
}

fn remove_files(id: &str) {
    for ext in ["json", "audio", "part", "type", "cover", "covertype"] {
        if let Some(file) = path(id, ext) {
            let _ = fs::remove_file(file);
        }
    }
}

#[tauri::command(async)]
pub fn offline_download(
    downloads: State<'_, Downloads>,
    id: String,
    url: String,
    cover_url: Option<String>,
    auth: String,
    meta: String,
    progress: Channel<Progress>,
) -> Result<(), String> {
    if !valid(&id) {
        return Err("invalid track".into());
    }
    let cancel = Arc::new(AtomicBool::new(false));
    if let Ok(mut active) = downloads.0.lock() {
        active.insert(id.clone(), cancel.clone());
    }
    let result = download(
        &id,
        &url,
        cover_url.as_deref(),
        &auth,
        &meta,
        &progress,
        &cancel,
    );
    if let Ok(mut active) = downloads.0.lock() {
        active.remove(&id);
    }
    if result.is_err() {
        remove_files(&id);
    }
    result
}

#[tauri::command]
pub fn offline_cancel(downloads: State<'_, Downloads>, id: String) {
    if let Ok(active) = downloads.0.lock()
        && let Some(cancel) = active.get(&id)
    {
        cancel.store(true, Ordering::Relaxed);
    }
}

#[tauri::command(async)]
pub fn offline_list() -> Result<Vec<String>, String> {
    let Some(dir) = dir() else {
        return Ok(Vec::new());
    };
    let Ok(entries) = fs::read_dir(&dir) else {
        return Ok(Vec::new());
    };
    let mut metas = Vec::new();
    for entry in entries.flatten() {
        let file = entry.path();
        if file.extension().is_some_and(|ext| ext == "json")
            && file.with_extension("audio").is_file()
            && let Ok(meta) = fs::read_to_string(&file)
        {
            metas.push(meta);
        }
    }
    Ok(metas)
}

#[tauri::command(async)]
pub fn offline_usage() -> Result<u64, String> {
    let Some(entries) = dir().and_then(|dir| fs::read_dir(dir).ok()) else {
        return Ok(0);
    };
    Ok(entries
        .flatten()
        .filter(|entry| entry.path().extension().is_some_and(|ext| ext == "audio"))
        .filter_map(|entry| entry.metadata().ok())
        .map(|meta| meta.len())
        .sum())
}

#[tauri::command(async)]
pub fn offline_update(id: String, meta: String) -> Result<(), String> {
    let (Some(json), Some(audio)) = (path(&id, "json"), path(&id, "audio")) else {
        return Ok(());
    };
    if json.is_file() && audio.is_file() {
        fs::write(json, meta).map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command(async)]
pub fn offline_remove(id: String) -> Result<(), String> {
    remove_files(&id);
    Ok(())
}

#[tauri::command(async)]
pub fn offline_clear(downloads: State<'_, Downloads>) -> Result<(), String> {
    if let Ok(active) = downloads.0.lock() {
        for cancel in active.values() {
            cancel.store(true, Ordering::Relaxed);
        }
    }
    if let Some(dir) = dir() {
        for file in own_files(&dir) {
            let _ = fs::remove_file(file);
        }
    }
    Ok(())
}

fn safe_cover_mime(raw: &str) -> &'static str {
    let base = raw.split(';').next().unwrap_or("").trim();
    if base.eq_ignore_ascii_case("image/png") {
        "image/png"
    } else if base.eq_ignore_ascii_case("image/webp") {
        "image/webp"
    } else if base.eq_ignore_ascii_case("image/gif") {
        "image/gif"
    } else {
        "image/jpeg"
    }
}

pub fn cover(request: Request<Vec<u8>>) -> Response<Vec<u8>> {
    let id = request.uri().path().trim_start_matches('/');
    let found = path(id, "cover").and_then(|file| fs::read(file).ok());
    let Some(bytes) = found else {
        return Response::builder()
            .status(StatusCode::NOT_FOUND)
            .body(Vec::new())
            .unwrap_or_default();
    };
    let stored_type = path(id, "covertype")
        .and_then(|file| fs::read_to_string(file).ok())
        .unwrap_or_default();
    Response::builder()
        .header(header::CONTENT_TYPE, safe_cover_mime(&stored_type))
        .header("X-Content-Type-Options", "nosniff")
        .header(header::CACHE_CONTROL, "max-age=3600")
        .body(bytes)
        .unwrap_or_default()
}
