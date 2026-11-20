// Self-update. The server names a newer build of this kind and its signature;
// Tauri's updater downloads it, checks the signature against the key built
// into this app, installs it, and the app restarts into it.

use std::sync::Mutex;

use serde::Serialize;
use tauri::utils::config::BundleType;
use tauri::utils::platform::bundle_type;
use tauri::{AppHandle, Config, Emitter, State, Url};
use tauri_plugin_updater::{Update, UpdaterExt};

const NOT_HERE: &str = "This install of Music doesn't update itself.";

// Whether this build carries the public half of the key its updates are signed
// with: plugins.updater.pubkey, which a signed build sets
// (docs/desktop.md → Updates). Without it the app only points at downloads.
pub fn has_key(config: &Config) -> bool {
    config
        .plugins
        .0
        .get("updater")
        .and_then(|updater| updater.get("pubkey"))
        .and_then(|key| key.as_str())
        .is_some_and(|key| !key.trim().is_empty())
}

// The server's name for this kind of install, when it is one that can replace
// itself. A .deb or .rpm belongs to the system's package manager.
pub fn kind(app: &AppHandle) -> Option<&'static str> {
    if !has_key(app.config()) {
        return None;
    }
    match bundle_type()? {
        BundleType::AppImage => Some("appimage"),
        BundleType::Nsis => Some("windows"),
        _ => None,
    }
}

#[derive(Default)]
pub struct Pending(Mutex<Option<Update>>);

impl Pending {
    fn set(&self, update: Option<Update>) {
        *self.0.lock().unwrap_or_else(|e| e.into_inner()) = update;
    }

    fn take(&self) -> Option<Update> {
        self.0.lock().unwrap_or_else(|e| e.into_inner()).take()
    }
}

#[derive(Serialize)]
pub struct Offer {
    version: String,
}

#[derive(Clone, Serialize)]
struct Progress {
    downloaded: u64,
    total: Option<u64>,
}

// The server's address goes along twice: the updater wants a full address for
// the download, and only the app knows the one it reaches its server under.
fn endpoint(server: &str, kind: &str, version: &str) -> Option<Url> {
    let server = server.trim_end_matches('/');
    let mut url = Url::parse(&format!("{server}/api/app/update")).ok()?;
    url.query_pairs_mut()
        .append_pair("platform", kind)
        .append_pair("current", version)
        .append_pair("base", server);
    Some(url)
}

#[tauri::command]
pub async fn update_check(
    app: AppHandle,
    pending: State<'_, Pending>,
    server: String,
    token: String,
) -> Result<Option<Offer>, String> {
    let kind = kind(&app).ok_or(NOT_HERE)?;
    let version = app.package_info().version.to_string();
    let endpoint = endpoint(&server, kind, &version).ok_or("The server's address isn't one.")?;
    let update = app
        .updater_builder()
        .endpoints(vec![endpoint])
        .and_then(|updater| updater.header("Authorization", format!("Bearer {token}")))
        .and_then(|updater| updater.build())
        .map_err(|e| e.to_string())?
        .check()
        .await
        .map_err(|e| e.to_string())?;
    let offer = update.as_ref().map(|update| Offer {
        version: update.version.clone(),
    });
    pending.set(update);
    Ok(offer)
}

#[tauri::command]
pub async fn update_install(app: AppHandle, pending: State<'_, Pending>) -> Result<(), String> {
    let update = pending.take().ok_or("There is no update to install.")?;
    let mut downloaded = 0;
    let events = app.clone();
    update
        .download_and_install(
            move |chunk, total| {
                downloaded += chunk as u64;
                let _ = events.emit("update-progress", Progress { downloaded, total });
            },
            || {},
        )
        .await
        .map_err(|e| format!("The update couldn't be installed: {e}"))?;
    crate::background::stop(&app);
    app.restart()
}

#[cfg(test)]
mod tests {
    use super::endpoint;

    #[test]
    fn endpoint_carries_the_install_kind_version_and_server() {
        let url = endpoint("https://music.example.org/sub/", "appimage", "0.4.0").unwrap();
        assert_eq!(url.path(), "/sub/api/app/update");
        let query: Vec<(String, String)> = url.query_pairs().into_owned().collect();
        assert_eq!(
            query,
            [
                ("platform".to_string(), "appimage".to_string()),
                ("current".to_string(), "0.4.0".to_string()),
                (
                    "base".to_string(),
                    "https://music.example.org/sub".to_string()
                ),
            ]
        );
        assert!(endpoint("not an address", "appimage", "0.4.0").is_none());
    }
}
