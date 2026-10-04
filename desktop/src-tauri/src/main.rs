#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod about;
mod audio;
mod background;
mod links;
mod media;
mod offline;
mod tray;
mod window;

use tauri::{Manager, RunEvent};

fn main() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            window::open(app)
        }))
        .plugin(tauri_plugin_deep_link::init())
        .manage(links::Links::default())
        .manage(background::Background::default())
        .manage(tray::Tray::default())
        .manage(offline::Downloads::default())
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
            window::window_close,
            window::window_hide,
            offline::offline_download,
            offline::offline_cancel,
            offline::offline_list,
            offline::offline_usage,
            offline::offline_update,
            offline::offline_remove,
            offline::offline_clear,
            about::app_about,
        ])
        .setup(|app| {
            offline::setup(app);
            let audio = audio::Audio::start(app.handle().clone());
            let media_events = audio.sender();
            app.manage(audio);
            let window = window::build(app.handle())?;
            media::setup(app.handle(), &window, media_events);
            links::setup(app);
            Ok(())
        })
        .build(tauri::generate_context!())
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
