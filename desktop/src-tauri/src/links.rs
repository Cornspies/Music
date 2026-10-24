use std::sync::Mutex;

use tauri::{App, AppHandle, Emitter, Manager, State, Url};
use tauri_plugin_deep_link::DeepLinkExt;

const SCHEME: &str = "com.cornspies.music";

#[derive(Default)]
pub struct Links(Mutex<Vec<String>>);

pub fn setup(app: &App) {
    let handle = app.handle().clone();
    app.deep_link()
        .on_open_url(move |event| receive(&handle, event.urls()));
    if let Ok(Some(urls)) = app.deep_link().get_current() {
        receive(app.handle(), urls);
    }
    #[cfg(target_os = "linux")]
    if let Some(appimage) = app.env().appimage {
        let handle = app.handle().clone();
        std::thread::spawn(move || {
            let _ = register_appimage(&handle, &appimage.to_string_lossy());
        });
    }
}

#[cfg(target_os = "linux")]
const HANDLER: &str = "music-desktop-handler.desktop";

#[cfg(target_os = "linux")]
fn register_appimage(app: &AppHandle, appimage: &str) -> std::io::Result<()> {
    use std::process::Command;

    if appimage.is_empty() || appimage.chars().any(|c| c.is_control()) {
        return Ok(());
    }

    let dir = app
        .path()
        .data_dir()
        .map_err(std::io::Error::other)?
        .join("applications");
    std::fs::create_dir_all(&dir)?;
    let entry = format!(
        "[Desktop Entry]\nType=Application\nName=Music\nExec={} %u\nTerminal=false\nNoDisplay=true\nMimeType=x-scheme-handler/{SCHEME};\n",
        exec_path(appimage)
    );
    let file = dir.join(HANDLER);
    if std::fs::read_to_string(&file).ok().as_deref() != Some(entry.as_str()) {
        std::fs::write(&file, entry)?;
        let _ = Command::new("update-desktop-database").arg(&dir).status();
    }
    let mime = format!("x-scheme-handler/{SCHEME}");
    let current = Command::new("xdg-mime")
        .args(["query", "default", &mime])
        .output()?;
    if String::from_utf8_lossy(&current.stdout).trim() != HANDLER {
        Command::new("xdg-mime")
            .args(["default", HANDLER, &mime])
            .status()?;
    }
    Ok(())
}

#[cfg(target_os = "linux")]
fn exec_path(path: &str) -> String {
    let plain = path
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || "/._-+,@".contains(c));
    if plain {
        return path.to_string();
    }
    let mut quoted = String::from("\"");
    for c in path.chars().filter(|c| !c.is_control()) {
        match c {
            '"' | '`' | '$' => {
                quoted.push_str("\\\\");
                quoted.push(c);
            }
            '\\' => quoted.push_str("\\\\\\\\"),
            '%' => quoted.push_str("%%"),
            _ => quoted.push(c),
        }
    }
    quoted.push('"');
    quoted
}

fn receive(app: &AppHandle, urls: Vec<Url>) {
    let urls: Vec<String> = urls
        .into_iter()
        .filter(|url| url.scheme() == SCHEME)
        .map(String::from)
        .collect();
    if urls.is_empty() {
        return;
    }
    if let Ok(mut pending) = app.state::<Links>().0.lock() {
        pending.extend(urls);
    }
    let _ = app.emit_to("main", "links", ());
    crate::window::open(app);
}

#[tauri::command]
pub fn take_links(links: State<'_, Links>) -> Vec<String> {
    links
        .0
        .lock()
        .map(|mut pending| std::mem::take(&mut *pending))
        .unwrap_or_default()
}
