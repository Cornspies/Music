import type { Metadata } from "next"
import type { ReactNode } from "react"
import Link from "next/link"
import { AudioLines } from "lucide-react"

const OPERATOR = "Cornspies"
const CONTACT = "contact@cornspies.com"
const SERVER = "music.cornspies.com"
const UPDATED = "8 October 2026"

export const metadata: Metadata = {
  title: "Privacy policy · Music",
  description: "What data Music handles, why, who can see it, and what you can do about it.",
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mt-12">
      <h2 className="text-xl font-semibold tracking-tight text-zinc-50">{title}</h2>
      <div className="mt-4 space-y-4">{children}</div>
    </section>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <h3 className="font-medium text-zinc-100">{title}</h3>
      {children}
    </div>
  )
}

function List({ children }: { children: ReactNode }) {
  return <ul className="list-disc space-y-1.5 pl-5 marker:text-zinc-600">{children}</ul>
}

function Mail() {
  return (
    <a
      href={`mailto:${CONTACT}`}
      className="font-medium text-emerald-400 hover:text-emerald-300 hover:underline"
    >
      {CONTACT}
    </a>
  )
}

export default function PrivacyPage() {
  return (
    <main className="min-h-dvh bg-zinc-950 px-4 pb-20 pt-10 text-[15px] leading-relaxed text-zinc-300 sm:pt-16">
      <article className="mx-auto max-w-2xl">
        <Link href="/" className="inline-flex items-center gap-2.5 font-semibold text-zinc-100">
          <span className="flex size-9 items-center justify-center rounded-xl bg-linear-to-br from-emerald-400 to-teal-600">
            <AudioLines className="size-5 text-zinc-950" aria-hidden="true" />
          </span>
          Music
        </Link>

        <h1 className="mt-10 text-3xl font-bold tracking-tight text-zinc-50">Privacy policy</h1>
        <p className="mt-2 text-sm text-zinc-500">Last updated {UPDATED}</p>

        <p className="mt-6">
          Music is a music player for iPhone, Android, Windows, Linux and the web, together with the music
          server at {SERVER}.
          It&apos;s run by {OPERATOR}. This policy explains what data Music handles, why, who can see it, how
          long it&apos;s kept and what you can do about it.
        </p>

        <div className="mt-8 space-y-3 rounded-xl border border-zinc-800 bg-zinc-900/60 p-5">
          <p className="font-medium text-zinc-100">In short</p>
          <List>
            <li>Music keeps only what it needs to work: your account, your music and what you listen to.</li>
            <li>No ads, no analytics, no tracking, and your data is never sold.</li>
            <li>Music never asks for your email address, phone number, real name, contacts or location.</li>
            <li>
              Your data is stored on a server in the <span className="whitespace-nowrap">EU/EEA</span>.
            </li>
            <li>You can delete your account in the app at any time.</li>
          </List>
        </div>

        <Section title="Who is responsible">
          <p>
            {OPERATOR} runs Music and is responsible for your personal data (the &ldquo;controller&rdquo; under
            the EU General Data Protection Regulation, the GDPR). For questions or requests about your data,
            email <Mail />.
          </p>
          <p>
            This policy covers the Music apps and the server at {SERVER}. The apps can also connect to a Music
            server run by someone else. On another server, its operator is responsible for the data stored
            there, and none of it reaches us.
          </p>
        </Section>

        <Section title="What data Music handles">
          <p>To use Music you need a username and a password. Everything else is up to you.</p>
          <Group title="Your account">
            <List>
              <li>
                Your username and password. The password is stored only as a secure hash, so nobody can read
                it, including us.
              </li>
              <li>When you created your account, and whether you&apos;re a member or an administrator.</li>
              <li>Your profile picture, if you add one.</li>
              <li>If you joined with an invite link, which link you used.</li>
            </List>
          </Group>
          <Group title="Your music">
            <List>
              <li>
                Songs you upload or add through Discover, with their details, such as title, artist, album and
                cover art. For playback, the server also works out things like each song&apos;s loudness and
                waveform, and it saves lyrics once they&apos;ve been looked up.
              </li>
              <li>Your playlists (private unless you make them public), favorites and pinned playlists.</li>
            </List>
          </Group>
          <Group title="What you listen to">
            <List>
              <li>
                Which songs you play, when, and for how long. This powers your history, play counts and
                recently played songs.
              </li>
              <li>
                The song that&apos;s playing, how far into it you are and your queue, so you can carry on
                on another device.
              </li>
            </List>
          </Group>
          <Group title="Sharing and Listen Together">
            <List>
              <li>
                Share links you create for a song: when you made them, when they expire and how often
                they&apos;ve been played.
              </li>
              <li>
                Listen Together sessions: who&apos;s in them, the queue and who added which song. Sessions
                exist only in the server&apos;s memory while they run and are never saved.
              </li>
            </List>
          </Group>
          <Group title="Technical data">
            <List>
              <li>
                Like any internet service, the server sees your device&apos;s IP address when it connects. To
                stop password guessing, it counts failed sign-ins per IP address and per account, in memory
                only, and forgets them after 15 minutes.
              </li>
              <li>
                The desktop app asks the server about twice a day whether a newer version is available, sending
                its version number and package type.
              </li>
              <li>
                Technical logs record which parts of the server were used, the result and how long it took,
                to keep Music secure and fix problems. The web server in front of Music may also log IP
                addresses and browser or app versions. Logs are deleted after at most 14 days.
              </li>
            </List>
          </Group>
          <Group title="What Music doesn't collect">
            <p>
              Music never asks for your email address, phone number, real name, contacts or location, and it
              doesn&apos;t use your camera or microphone. There are no ads and no analytics or tracking tools,
              and nothing is used to follow you across other apps or websites.
            </p>
          </Group>
        </Section>

        <Section title="What stays on your device">
          <p>Some data stays on your phone or computer and is never sent to us:</p>
          <List>
            <li>
              Your sign-in, kept in the iPhone Keychain, in encrypted storage on Android, in the
              browser&apos;s local storage on the web, or in the desktop app&apos;s own storage on Windows and
              Linux.
            </li>
            <li>
              Songs you download for offline listening, cached cover art and your settings. Signing out
              removes downloads from your phone, and uninstalling the app removes everything.
            </li>
            <li>
              In the desktop app, downloaded songs and their cover art are saved as files in the app&apos;s
              data folder (<span className="break-all">~/.local/share/com.cornspies.music/offline</span> on
              Linux, <span className="break-all">%APPDATA%\com.cornspies.music\offline</span> on Windows).
              They stay there when you sign out, until you remove them on the Downloads page or delete the
              folder.
            </li>
            <li>
              The song that&apos;s playing, which your phone shows on its lock screen, in its media controls and,
              on Android, in Music&apos;s home screen widget. On a computer, the desktop app shows it in the
              system&apos;s media controls (the media overlay on Windows, MPRIS on Linux), where other apps on
              that computer can read it.
            </li>
            <li>
              On Android, Music asks for permission to show notifications, for its playback controls. On
              iPhone, choosing a profile picture opens the system photo picker, which gives Music only the
              photo you pick.
            </li>
          </List>
          <p>The website doesn&apos;t use cookies.</p>
        </Section>

        <Section title="Why Music uses your data">
          <List>
            <li>
              To provide Music to you (GDPR Article 6(1)(b), performing our agreement with you): your
              account, your music and playlists, your history, syncing between devices, sharing and Listen
              Together.
            </li>
            <li>
              For our legitimate interests (Article 6(1)(f)): keeping Music secure and running, such as
              blocking password guessing, keeping logs and fixing errors, and completing songs&apos; details,
              cover art and lyrics.
            </li>
            <li>To comply with the law (Article 6(1)(c)), where that applies.</li>
          </List>
          <p>
            Your data isn&apos;t used for advertising or profiling, it&apos;s never sold, and no decisions
            about you are made automatically.
          </p>
        </Section>

        <Section title="Who can see your data">
          <Group title="Other members of the server">
            <p>Everyone with an account on the server shares one music library. Other members can see:</p>
            <List>
              <li>songs you uploaded or added, with your username and profile picture;</li>
              <li>playlists you make public;</li>
              <li>
                your username, profile picture and the songs you add when you&apos;re in a Listen Together
                session with them.
              </li>
            </List>
            <p>Your private playlists, favorites and listening history are only visible to you.</p>
          </Group>
          <Group title="People with a link">
            <p>
              Anyone with a share link to a song can play it without an account, and sees its title, artist,
              album, cover art and your username as the person who shared it. That works until the link
              expires or you remove it. People who open an invite link see the username of the administrator
              who created it.
            </p>
          </Group>
          <Group title="Administrators">
            <p>
              The server&apos;s administrators can see the list of accounts, with each account&apos;s
              username, sign-up date, number of uploads, storage used and when it last played a song. They can
              sign accounts out, change their role, delete them, and remove share and invite links. As the
              operator, {OPERATOR} also has technical access to the server and uses it only to run and protect
              Music.
            </p>
          </Group>
          <Group title="Music services the server contacts">
            <p>
              To complete song details, cover art and lyrics, and to find music in Discover, the server
              contacts these services itself:
            </p>
            <List>
              <li>LRCLIB, for lyrics;</li>
              <li>
                MusicBrainz and the Cover Art Archive, Deezer and Apple&apos;s iTunes Search, for song details
                and cover art;
              </li>
              <li>
                YouTube (Google), SoundCloud and NCS (NoCopyrightSounds), for finding and adding music in
                Discover, and for song details.
              </li>
            </List>
            <p>
              They receive only song details (such as title, artist and album), what you search for, or links
              you import. They never receive your account details, and because the requests come from the
              server, they don&apos;t see your IP address. These are independent services with their own
              privacy policies, and some are based outside the EU, for example in the United States.
            </p>
          </Group>
          <Group title="Discord">
            <p>
              If you turn on Discord in the desktop app&apos;s settings (it&apos;s off unless you do), the app
              passes the title, artist and album of the song you&apos;re playing to the Discord app on your
              computer. Discord then shows it on your profile to the people who can see your status, under
              Discord&apos;s own privacy policy. Nothing else is sent, and turning it off clears your status.
            </p>
          </Group>
          <Group title="Apple and Google">
            <p>
              If you get Music from the App Store or Google Play, Apple or Google handle that under their own
              privacy policies. If you&apos;ve agreed in your phone&apos;s settings to share crash reports
              with app developers, they may pass anonymous crash reports on to us.
            </p>
          </Group>
          <Group title="Authorities">
            <p>We only give data to authorities when the law requires us to.</p>
          </Group>
        </Section>

        <Section title="Where your data is stored">
          <p>
            The server is located in the <span className="whitespace-nowrap">EU/EEA</span>, and we don&apos;t
            transfer your personal data outside it. The requests to music services described above contain
            nothing that identifies you.
          </p>
        </Section>

        <Section title="How long data is kept">
          <List>
            <li>Your account, music, playlists, favorites and listening history: as long as you have an account.</li>
            <li>Share links: until they expire or you remove them.</li>
            <li>Listen Together sessions: only while they run.</li>
            <li>Failed sign-in counts: 15 minutes.</li>
            <li>Technical logs: at most 14 days.</li>
            <li>Backups of the server: at most 30 days, so deleted data disappears from them within that time.</li>
          </List>
          <p>
            When you delete your account, your account, profile picture, playlists, favorites, listening
            history and share links are deleted immediately. Songs you uploaded stay in the shared library but
            are no longer linked to you, as the app explains when you delete your account. To have them removed
            as well, delete them before you delete your account (only you can delete your uploads), or email
            us.
          </p>
        </Section>

        <Section title="Your rights">
          <p>Under the GDPR, you have the right to:</p>
          <List>
            <li>see the data we hold about you and get a copy of it;</li>
            <li>have it corrected (you can change your username yourself);</li>
            <li>
              have it deleted (in the app: Profile, then Delete account; on the web: Account, then Delete
              account);
            </li>
            <li>restrict or object to how we use it;</li>
            <li>receive it in a format you can take elsewhere.</li>
          </List>
          <p>
            To use these rights, email <Mail />. We&apos;ll answer within one month. You can also complain to a
            data protection authority, for example the one in the country where you live.
          </p>
        </Section>

        <Section title="Security">
          <p>
            Connections to the server are encrypted (HTTPS), passwords are stored only as secure hashes,
            sign-ins on phones are kept in the system&apos;s secure storage, and repeated wrong passwords are
            blocked. No service can guarantee perfect security, but we work to keep your data safe.
          </p>
        </Section>

        <Section title="Children">
          <p>
            Music isn&apos;t meant for children under 16. If you believe a child under 16 has an account,
            email us and we&apos;ll delete it.
          </p>
        </Section>

        <Section title="Changes to this policy">
          <p>If this policy changes, we&apos;ll publish the new version here and update the date at the top.</p>
        </Section>

        <Section title="Contact">
          <p>
            {OPERATOR} · <Mail />
          </p>
        </Section>
      </article>
    </main>
  )
}
