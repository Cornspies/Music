// The window's icon on Linux. Wayland compositors (KWin, GNOME Shell) ignore
// the icon a window sets on itself: they take the window's app ID, find the
// .desktop file of that name and show its Icon=, in the taskbar and in the
// title bar alike. GTK uses the program name as the app ID.

use std::ffi::OsString;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use tauri::utils::config::BundleType;
use tauri::utils::platform::bundle_type;
use tauri::{App, Manager};

// The binary's name, set explicitly so a renamed copy keeps its icon. The
// AppImage's own entry names it as its StartupWMClass too, so launchers made
// from that entry match the window.
const APP_ID: &str = "music-desktop";
// The .deb and .rpm install Music.desktop: Tauri names the entry after the
// product, and installs the icons as music-desktop.png.
const PACKAGED_APP_ID: &str = "Music";

const ICON: &[u8] = include_bytes!("../icons/128x128@2x.png");

fn packaged() -> bool {
    matches!(bundle_type(), Some(BundleType::Deb | BundleType::Rpm))
}

// Has to run before GTK starts.
pub fn set_app_id() {
    let id = if packaged() { PACKAGED_APP_ID } else { APP_ID };
    webkit2gtk::glib::set_prgname(Some(id));
}

// The AppImage and a plain build come without an entry, so before the window
// opens one is written to ~/.local/share/applications, hidden from the app
// menu, with the icon in ~/.local/share/icons.
pub fn install(app: &App) {
    if packaged() {
        return;
    }
    let exec = match app.env().appimage {
        Some(appimage) => appimage,
        None => match std::env::current_exe() {
            Ok(exe) => exe.into_os_string(),
            Err(_) => return,
        },
    };
    if let Ok(data) = app.path().data_dir() {
        let _ = write_entry(&data, exec);
    }
}

fn write_entry(data: &Path, exec: OsString) -> io::Result<()> {
    let exec = exec.to_string_lossy();
    // A rebuilt binary leaves the running one as "... (deleted)".
    if exec.chars().any(char::is_control) || !Path::new(exec.as_ref()).exists() {
        return Ok(());
    }
    if installed_elsewhere(data) {
        return Ok(());
    }
    // The icon's path rather than its name: Plasma doesn't always find an icon
    // name that was installed while it runs, and the title bar or the taskbar
    // then stays generic until the next login. A path it reads as it is.
    let icon = data
        .join("icons/hicolor/256x256/apps")
        .join(format!("{APP_ID}.png"));
    write_if_changed(&icon, ICON)?;
    let entry = format!(
        "[Desktop Entry]\nType=Application\nName=Music\nComment=Music player for your Music server\nExec={}\nIcon={}\nTerminal=false\nNoDisplay=true\n",
        crate::links::exec_path(&exec),
        icon.to_str().unwrap_or(APP_ID)
    );
    write_if_changed(&entry_path(data), entry.as_bytes())
}

// An entry of the same name in a system directory, from a package someone made
// themselves, would be hidden from the app menu by a user one.
fn installed_elsewhere(data: &Path) -> bool {
    let dirs = std::env::var_os("XDG_DATA_DIRS")
        .filter(|dirs| !dirs.is_empty())
        .unwrap_or_else(|| "/usr/local/share:/usr/share".into());
    std::env::split_paths(&dirs)
        .filter(|dir| dir.is_absolute() && dir != data)
        .any(|dir| entry_path(&dir).exists())
}

fn entry_path(data: &Path) -> PathBuf {
    data.join("applications").join(format!("{APP_ID}.desktop"))
}

fn write_if_changed(path: &Path, contents: &[u8]) -> io::Result<()> {
    if fs::read(path).is_ok_and(|current| current == contents) {
        return Ok(());
    }
    if let Some(dir) = path.parent() {
        fs::create_dir_all(dir)?;
    }
    fs::write(path, contents)
}
