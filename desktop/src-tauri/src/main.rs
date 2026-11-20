#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod about;
mod audio;
mod background;
#[cfg(target_os = "linux")]
mod desktop_entry;
mod discord;
mod links;
mod media;
mod offline;
mod settings;
mod tray;
mod update;
mod window;

use tauri::{Manager, RunEvent};

fn main() {
    #[cfg(target_os = "linux")]
    if std::env::var_os("WEBKIT_USE_SKIA_FOR_COMPOSITION").is_none() {
        unsafe { std::env::set_var("WEBKIT_USE_SKIA_FOR_COMPOSITION", "0") };
    }
    #[cfg(target_os = "linux")]
    desktop_entry::set_app_id();
    let context = tauri::generate_context!();
    let mut builder =
        tauri::Builder::default().plugin(tauri_plugin_single_instance::init(|app, _, _| {
            window::open(app)
        }));
    if update::has_key(context.config()) {
        builder = builder.plugin(tauri_plugin_updater::Builder::new().build());
    }
    let app = builder
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(
            tauri_plugin_autostart::Builder::new()
                .args([settings::MINIMIZED_ARG])
                .build(),
        )
        .manage(links::Links::default())
        .manage(background::Background::default())
        .manage(tray::Tray::default())
        .manage(offline::Downloads::default())
        .manage(update::Pending::default())
        .register_uri_scheme_protocol("music-cover", |_, request| offline::cover(request))
        .invoke_handler(tauri::generate_handler![
            audio::audio_init,
            audio::audio_load,
            audio::audio_prepare,
            audio::audio_play,
            audio::audio_pause,
            audio::audio_seek,
            audio::audio_volume,
            audio::audio_gains,
            audio::audio_crossfade,
            audio::audio_stop,
            audio::audio_info,
            audio::audio_preview,
            audio::audio_preview_stop,
            media::media_metadata,
            links::take_links,
            background::background_start,
            background::background_take,
            window::window_hide,
            window::window_tray,
            window::app_quit,
            settings::settings_get,
            settings::settings_set,
            settings::settings_open_downloads,
            settings::settings_pick_downloads,
            settings::settings_reset_downloads,
            offline::offline_download,
            offline::offline_cancel,
            offline::offline_list,
            offline::offline_missing_thumbs,
            offline::offline_thumb,
            offline::offline_usage,
            offline::offline_update,
            offline::offline_remove,
            offline::offline_clear,
            about::app_about,
            update::update_check,
            update::update_install,
        ])
        .setup(|app| {
            let settings = settings::setup(app);
            offline::setup(app.handle(), settings.download_dir.clone());
            let audio = audio::Audio::start(app.handle().clone());
            let media_events = audio.sender();
            app.manage(audio);
            let minimized = settings.start_minimized && settings::launched_minimized();
            tray::show(app.handle());
            #[cfg(target_os = "linux")]
            desktop_entry::install(app);
            let window = window::start(app.handle(), minimized)?;
            media::setup(app.handle(), window.as_ref(), media_events);
            discord::start(settings.discord);
            links::setup(app);
            Ok(())
        })
        .build(context)
        .expect("error while building Music");
    app.run(|_, event| {
        if let RunEvent::ExitRequested { api, code, .. } = event
            && code.is_none()
            && window::keeps_running()
        {
            api.prevent_exit();
        }
    });
}
