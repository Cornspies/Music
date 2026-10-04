use serde::Serialize;
use tauri::AppHandle;
use tauri::utils::config::BundleType;
use tauri::utils::platform::bundle_type;

#[derive(Serialize)]
pub struct About {
    version: String,
    bundle: Option<&'static str>,
}

#[tauri::command]
pub fn app_about(app: AppHandle) -> About {
    let bundle = bundle_type().map(|kind| match kind {
        BundleType::AppImage => "appimage",
        BundleType::Deb => "deb",
        BundleType::Rpm => "rpm",
        BundleType::Nsis => "windows",
        BundleType::Msi => "msi",
        _ => "other",
    });
    About {
        version: app.package_info().version.to_string(),
        bundle,
    }
}
