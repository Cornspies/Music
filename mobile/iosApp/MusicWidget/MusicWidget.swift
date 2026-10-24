import WidgetKit
import SwiftUI

private let appGroup = "group.com.cornspies.music"

struct NowPlayingEntry: TimelineEntry {
    let date: Date
    let title: String
    let artist: String
    let hasItem: Bool
}

struct NowPlayingProvider: TimelineProvider {
    func placeholder(in context: Context) -> NowPlayingEntry {
        NowPlayingEntry(date: .now, title: "Music", artist: "Nothing playing", hasItem: false)
    }

    func getSnapshot(in context: Context, completion: @escaping (NowPlayingEntry) -> Void) {
        completion(read())
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<NowPlayingEntry>) -> Void) {
        let next = Calendar.current.date(byAdding: .minute, value: 15, to: .now) ?? .now
        completion(Timeline(entries: [read()], policy: .after(next)))
    }

    private func read() -> NowPlayingEntry {
        let defaults = UserDefaults(suiteName: appGroup)
        return NowPlayingEntry(
            date: .now,
            title: defaults?.string(forKey: "title") ?? "",
            artist: defaults?.string(forKey: "artist") ?? "",
            hasItem: defaults?.bool(forKey: "hasItem") ?? false
        )
    }
}

struct MusicWidgetView: View {
    let entry: NowPlayingEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Image(systemName: entry.hasItem ? "waveform" : "music.note")
                .font(.title2)
                .foregroundStyle(Color(red: 0.2, green: 0.83, blue: 0.6))
            Spacer(minLength: 0)
            Text(entry.hasItem ? (entry.title.isEmpty ? "Untitled" : entry.title) : "Music")
                .font(.headline)
                .lineLimit(1)
            Text(entry.hasItem ? (entry.artist.isEmpty ? "Unknown artist" : entry.artist) : "Nothing playing")
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .padding()
        .containerBackground(Color(red: 0.035, green: 0.035, blue: 0.043), for: .widget)
        .widgetURL(URL(string: "cornspies-music://nowplaying"))
    }
}

@main
struct MusicWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "MusicWidget", provider: NowPlayingProvider()) { entry in
            MusicWidgetView(entry: entry)
        }
        .configurationDisplayName("Now playing")
        .description("What's playing in Music.")
        .supportedFamilies([.systemSmall, .systemMedium])
    }
}
