# Kontur Talk for Mattermost

[![CI](https://github.com/moonssword/mattermost-plugin-ktalk/actions/workflows/ci.yml/badge.svg)](https://github.com/moonssword/mattermost-plugin-ktalk/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Mattermost 11.7+](https://img.shields.io/badge/Mattermost-11.7%2B-1c58d9.svg)

**English** · [Русский](README.ru.md)

A Mattermost plugin for [Kontur Talk](https://kontur.ru/talk) (Контур.Толк), a Russian video conferencing service. Start and join meetings from any channel, ring colleagues in direct messages, schedule meetings in the Talk calendar, and get recordings, transcripts and AI summaries back in the meeting thread.

> [!NOTE]
> This is an independent, community-built plugin. It is not affiliated with, endorsed by or supported by SKB Kontur. Kontur Talk is a trademark of its owner.

![Meeting cards in a channel and the plugin sidebar](docs/screenshots/cards-light.png)

## Features

- **One-click meetings.** A channel header button, `/talk start [topic]`, a button in the channel intro, and *Discuss in Talk* in the message menu.
- **Live meeting cards.** Status, duration, who is in the room, PIN, dial-in numbers, and *Join*, *Start recording* and *End for everyone* buttons.
- **Calls in direct and group messages.** The call button offers to ring or only send a meeting link. Recipients of a call get it inside Mattermost: a ringing window, a desktop notification and a mobile push. People with a Talk account can also be rung in the Talk app. Answers show up on the caller's card.
- **Scheduling.** `/talk schedule` with time, recurrence chosen as in Talk (weekdays, every N days, weeks or months on chosen days, until a date or for a number of meetings), participants, external guests, PIN, waiting room and auto-recording. The card tracks invitees' answers, and the bot posts a reminder before the start.
- **Answering invitations.** With a personal Talk key, invitees accept or decline right in the attendee list on the card. For a recurring meeting the answer applies to the whole series.
- **Recordings and summaries.** The meeting thread is told when the recording and the summary are ready; only the author and participants can open them. The *Recordings* tab lists them for a chosen period of up to a month; minutes and a searchable transcript open in the sidebar.
- **Sidebar.** *Now*, *Upcoming* and *Recordings* tabs, quick start and a personal room. Recurring meetings collapse into one row, and your own meetings can be edited from *Upcoming*.
- **Presence.** While someone is in a meeting, their Mattermost status reads "In a Kontur Talk meeting". The profile popover shows a colleague's Talk status and a *Call in Talk* button.
- **Three visual styles,** in light and dark themes, chosen by the administrator and overridable per user.

| Upcoming meetings | Recording transcript |
|---|---|
| ![Upcoming tab with a recurring meeting collapsed into one row](docs/screenshots/sidebar-upcoming.png) | ![Transcript with speakers and timestamps](docs/screenshots/sidebar-transcript.png) |

The user interface is in Russian, with an English translation for users whose Mattermost language is not Russian. System Console settings and bot messages are in Russian.

## Requirements

- Mattermost Server 11.7 or later.
- A cloud Kontur Talk space (`https://<space>.ktalk.ru`, reachable over HTTPS) and an administrator account in it to create an API key.
- Optional: a Mattermost site URL reachable from the internet, for webhooks.

## Installation

1. Download `konturtalk-<version>.tar.gz` from [Releases](https://github.com/moonssword/mattermost-plugin-ktalk/releases), or build it with `make bundle`.
2. In Mattermost, go to **System Console → Plugins → Plugin Management** and upload the bundle.
3. In Kontur Talk, create a key under **Administration → API keys → Add**. The key is shown only once, for one hour.
4. In **System Console → Plugins → Контур.Толк**, enter the space address and the key. The *Connection* block shows the key's expiry and any missing scopes.

Without a key the plugin works in link-only mode: it creates a room link but does not manage the room.

### API key scopes

| Scope | Access | Used for |
|---|---|---|
| `rooms` | read & write | rooms, PIN, waiting room, calls |
| `profiles` | read | matching users by email, presence |
| `calendar` | read & write | scheduling |
| `recording` | read & write | recordings, transcripts, summaries |
| `activeRecordings` | read & write | the *Start recording* button |
| `reporting` | read | meeting results, finished conferences |
| `onlineStats` | read | usage counters and plan expiry in the console |
| `webhooks` | read & write | optional, see [Status updates](#status-updates) |

Grant only these scopes; the plugin needs nothing else.

## How it works

### Who may do what

Rights come from Talk, not from Mattermost roles: channel admins and system admins get nothing extra.

- **Running a meeting** (recording, ending for everyone, PIN, guest access, waiting room) is for the moderators of its room in Talk.
- **Changing or cancelling a scheduled meeting** is for the organizer of its event in Talk.
- **Answering an invitation** is for attendees of the event in Talk, with a personal key.
- **Stopping a call that rings** is for the person calling.

Every action is checked against Talk at the moment it is made; the buttons on a card only reflect it.

### Acting on behalf of users

Talk has no OAuth for third-party apps, so the plugin relies on the space key and on email matching:

1. **The space key** manages rooms, calls and recordings. Every request names the real person involved: the room moderator, the caller or the recording initiator.
2. **Mattermost users are matched to Talk users by email.** By default only verified addresses count; otherwise anyone could set a colleague's address and reach their calendar. Addresses of users who sign in through SAML, LDAP or OAuth come from the identity provider and cannot be changed in Mattermost, so they always count as verified. For password accounts on a server that cannot send email, verify addresses with `mmctl user verify <username>`. The address in Mattermost must be the same as in Talk.
3. **The calendar** is opened by email through the space key, so users do not need to sign in to anything.

**Personal Talk keys** are optional. A user issues one in Talk (*Settings → API keys*) and pastes it in **Settings → Контур.Толк** or on the *Upcoming* tab. With it, meetings are created in the user's own calendar under their name, and invitations can be answered from Mattermost. The plugin checks that the key belongs to the same Talk user, stores it encrypted, and disconnects it if Talk stops accepting it. The *Calendar with personal keys only* setting forbids scheduling through the space key for users without one.

**Recordings follow Talk's own access rules.** Starting a recording from a card asks who may open it, with the same choices as Talk; the plugin applies the choice when the recording is finished, since Talk does not accept it earlier. Stopping or pausing a recording is possible only in Talk: its API has no such method. For every recording the plugin asks Talk who holds a role on it (the author, the meeting's participants and anyone the owner shared it with) and shows the recording, summary, minutes and transcript only to them. Channel membership grants nothing, and system administrators get no exception. The meeting thread receives a notice that a summary is ready, never the text itself. By default the plugin leaves a new recording's link access as Talk set it; *Recording link access* can open it to the whole space, which makes the link posted in the channel work for everyone there.

### Status updates

The plugin polls the Talk API every 30 seconds: a card turns live within half a minute of the first person joining and closes after the room has been empty for two checks in a row. No extra setup is required.

Webhooks make updates near-instant. They need the `webhooks` scope (Kontur Talk support enables it on request), a Mattermost site URL reachable from the internet, and the *Use webhooks* setting followed by *Register webhook* in the *Connection* block. Deliveries are authenticated with a secret header. Talk allows ten webhook creations per space per day, so the plugin reuses an existing registration.

### Calendars

The plugin schedules through the Kontur Talk calendar and never connects to mail calendars directly. The Talk space administrator can make Exchange / Microsoft 365 or a CalDAV calendar the space's primary calendar; meetings scheduled from Mattermost then appear there, and answers given there show up on the card. Answering from Mattermost works only when the space uses the Talk calendar.

### Test mode

Test mode limits the plugin to the users listed in *Testers*. System administrators are not included by default, since a large organisation may have many of them; the *Access for system administrators* setting adds them all. Everyone else sees no buttons or sidebar, receives no calls, and commands and the API refuse them. The System Console keeps working for administrators either way. Cards that were already posted stay visible to the channel, so test in a separate channel.

## Visual styles

The *Default style* setting picks the look of cards and the sidebar; users can choose their own in **Settings → Контур.Толк** or with `/talk settings style cards`.

| System | Compact, dark theme |
|---|---|
| ![System style](docs/screenshots/current-light.png) | ![Compact style](docs/screenshots/compact-dark.png) |

## Commands

`/talk help` lists all commands. `pin`, `guests`, `lobby`, `record` and `end` act on the channel's latest active meeting and are available to the moderators of its room in Talk. `/talk connect` opens the personal key form.

## Working alongside Mattermost Calls

The plugins do not interfere: Calls keeps its `/call` command, posts and calls. With both installed, Mattermost merges the two call buttons in the channel header into one menu. Mattermost labels that menu "Call" in every language; the label comes from Mattermost itself and cannot be translated by plugins.

## Known limitations

- Recording can start only while a registered Talk user is in the room; this is how the Talk API works.
- Meeting results reach Talk's history a few minutes after the end. Conferences with guests only are not recorded in history.
- Talk cannot turn a recurring meeting into a single one or back; the plugin suggests cancelling and scheduling again.
- `/talk` stays in autocomplete for everyone, even in test mode: Mattermost cannot hide a command per user.

## Development

The server is written in Go and builds in a container, so a local Go toolchain is not required. The web app is React and TypeScript. See [CONTRIBUTING.md](CONTRIBUTING.md) for the layout and conventions.

```bash
make test        # server tests (in Docker) and web app tests
make lint        # golangci-lint and TypeScript checks
make bundle      # dist/konturtalk-<version>.tar.gz
make deploy      # build and install into a local Docker Mattermost via mmctl --local
make talk-check  # expiry and scopes of the key in .env (see .env.example)
```

Talk API reference: [developer.kontur.ru/doc/talk.public.api](https://developer.kontur.ru/doc/talk.public.api).

## Security

Please report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
