use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{App, AppHandle, Manager, State};
use tauri_plugin_autostart::ManagerExt;
use tauri_plugin_dialog::DialogExt;

use crate::{discord, offline};

pub const MINIMIZED_ARG: &str = "--minimized";

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Settings {
    pub close_to_tray: bool,
    pub start_minimized: bool,
    pub discord: bool,
    pub download_dir: Option<PathBuf>,
}

impl Default for Settings {
    fn default() -> Self {
        Settings {
            close_to_tray: false,
            start_minimized: true,
            discord: false,
            download_dir: None,
        }
    }
}

pub struct Store {
    file: Option<PathBuf>,
    settings: Mutex<Settings>,
}

impl Store {
    pub fn get(&self) -> Settings {
        self.settings.lock().map(|s| s.clone()).unwrap_or_default()
    }

    fn update(&self, change: impl FnOnce(&mut Settings)) -> Settings {
        let Ok(mut settings) = self.settings.lock() else {
            return Settings::default();
        };
        change(&mut settings);
        if let Some(file) = &self.file
            && let Ok(json) = serde_json::to_string_pretty(&*settings)
        {
            if let Some(dir) = file.parent() {
                let _ = fs::create_dir_all(dir);
            }
            let _ = fs::write(file, json);
        }
        settings.clone()
    }
}

pub fn setup(app: &App) -> Settings {
    let file = app
        .path()
        .app_config_dir()
        .ok()
        .map(|dir| dir.join("settings.json"));
    let settings: Settings = file
        .as_ref()
        .and_then(|f| fs::read_to_string(f).ok())
        .and_then(|json| serde_json::from_str(&json).ok())
        .unwrap_or_default();
    app.manage(Store {
        file,
        settings: Mutex::new(settings.clone()),
    });
    settings
}

pub fn get(app: &AppHandle) -> Settings {
    app.state::<Store>().get()
}

pub fn launched_minimized() -> bool {
    std::env::args().any(|arg| arg == MINIMIZED_ARG)
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    close_to_tray: bool,
    start_minimized: bool,
    autostart: bool,
    discord: bool,
    discord_available: bool,
    discord_connected: bool,
    download_dir: String,
    default_download_dir: bool,
}

fn view(app: &AppHandle) -> View {
    let settings = get(app);
    View {
        close_to_tray: settings.close_to_tray,
        start_minimized: settings.start_minimized,
        autostart: app.autolaunch().is_enabled().unwrap_or(false),
        discord: settings.discord,
        discord_available: discord::available(),
        discord_connected: discord::connected(),
        download_dir: offline::dir()
            .map(|dir| dir.display().to_string())
            .unwrap_or_default(),
        default_download_dir: settings.download_dir.is_none(),
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Change {
    close_to_tray: Option<bool>,
    start_minimized: Option<bool>,
    autostart: Option<bool>,
    discord: Option<bool>,
}

#[tauri::command]
pub fn settings_get(app: AppHandle) -> View {
    view(&app)
}

#[tauri::command]
pub fn settings_set(
    app: AppHandle,
    store: State<'_, Store>,
    change: Change,
) -> Result<View, String> {
    if let Some(on) = change.autostart {
        let autolaunch = app.autolaunch();
        if on {
            autolaunch.enable()
        } else {
            autolaunch.disable()
        }
        .map_err(|e| format!("Couldn't change autostart: {e}"))?;
    }
    let settings = store.update(|s| {
        if let Some(on) = change.close_to_tray {
            s.close_to_tray = on;
        }
        if let Some(on) = change.start_minimized {
            s.start_minimized = on;
        }
        if let Some(on) = change.discord {
            s.discord = on;
        }
    });
    if change.discord.is_some() {
        discord::set_enabled(settings.discord);
    }
    Ok(view(&app))
}

#[tauri::command(async)]
pub fn settings_open_downloads() -> Result<(), String> {
    let dir = offline::dir().ok_or("There's no downloads folder.")?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    tauri_plugin_opener::open_path(&dir, None::<&str>).map_err(|e| e.to_string())
}

#[tauri::command(async)]
pub fn settings_pick_downloads(
    app: AppHandle,
    store: State<'_, Store>,
    downloads: State<'_, offline::Downloads>,
) -> Result<Option<View>, String> {
    let Some(picked) = app
        .dialog()
        .file()
        .set_title("Choose where to keep downloaded songs")
        .blocking_pick_folder()
    else {
        return Ok(None);
    };
    let picked = picked.into_path().map_err(|e| e.to_string())?;
    let target = offline::folder_inside(&picked);
    offline::move_to(&downloads, &target)?;
    store.update(|s| s.download_dir = Some(target));
    Ok(Some(view(&app)))
}

#[tauri::command(async)]
pub fn settings_reset_downloads(
    app: AppHandle,
    store: State<'_, Store>,
    downloads: State<'_, offline::Downloads>,
) -> Result<View, String> {
    let target = offline::default_dir(&app).ok_or("There's no default downloads folder.")?;
    offline::move_to(&downloads, &target)?;
    store.update(|s| s.download_dir = None);
    Ok(view(&app))
}
