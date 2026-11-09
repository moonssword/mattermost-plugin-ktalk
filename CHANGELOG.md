# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.5.0] - 2026-11-09

First public release.

### Added

- Personal Talk keys can be connected from the interface: in **Settings → Контур.Толк** and on the *Upcoming* tab, as well as with `/talk connect`. Meetings scheduled with a personal key are created in the user's own calendar under their name.
- Invitees answer a scheduled meeting right in the attendee list on the card, using an *Accept / Decline* switch in their own row. An answer to a recurring meeting applies to the whole series.
- An *Edit* button on the *Upcoming* tab for meetings you can manage: as the organiser, a channel admin or a system admin.
- Loading placeholders shaped like the content replace the "Loading…" text. They fade in after a short delay and respect reduced motion.
- Test mode, modelled on Mattermost Calls: while it is on, only system admins and listed testers can use the plugin.

### Changed

- A recurring meeting takes one row on the *Upcoming* tab, showing its next date and frequency.
- The card shows how a meeting repeats (daily, weekly, monthly) instead of a generic "recurring".
- The sidebar header always fits on one line: on a narrow sidebar only *Start a meeting* keeps its label, and from 390 px all three buttons carry short labels.
- List rows in the sidebar put compact actions to the right of the title, so more meetings fit on screen.
- The personal key notice on the *Upcoming* tab is a single quiet line, with the explanation behind an info icon.
- Transcript lines show the speaker in bold with a colour dot and the time on the right. Speakers get distinct colours in order of appearance.
- The compact style uses one column for transcripts and adds spacing above tab lists.
- The incoming call window sits under the channel header and inside the channel column, clear of the header and the right-hand sidebar. It comes in all three visual styles.
- Unanswered calls close as "did not take place" two minutes after ringing stops; answered calls wait ten minutes for someone to join.
- The meeting status emoji matches the plugin's camera icon.
- Webhook event types are matched case-insensitively, as Talk spells them both ways.

### Fixed

- Editing a scheduled meeting removed its participants: Mattermost 11 drops preselected users in its standard user picker. The participant field now searches through the plugin and keeps existing participants.
- Changes to recurrence, guest access and auto-recording made in the edit form were not applied or not remembered.
- Removing every participant or external address in the edit form left them in the meeting.
- After accepting a recurring meeting, its occurrences stayed declined and hidden from the invitee's calendar if they had declined before. The plugin now accepts the upcoming occurrences too.
- An answer to a recurring meeting did not show on the card.
- *Schedule a meeting*, *Edit* and the topic prompt did nothing in Mattermost 11.
- Plugin buttons, the sidebar and calls were missing after signing in without a page reload.
- The channel intro showed two *Start a meeting* buttons.
- The repeat count in the scheduling form was always rejected in Mattermost 11.
- Personal keys were refused when used for the calendar, which has its own API for personal keys.
- Deleted meetings that Talk still lists in a personal calendar appeared on the *Upcoming* tab.
- The *Upcoming* tab did not refresh after a meeting was created, edited or cancelled.
- The "Set a status" dialog showed a blank screen because of a status saved by 0.4.0. Damaged recent statuses are repaired automatically.

## [0.4.0]

### Added

- Incoming calls ring inside Mattermost: a ringing window, a desktop notification and a mobile push, with *Decline* and *Join*. Answers appear on the caller's card, and ringing stops in every open tab.
- Coexistence with Mattermost Calls: both call buttons are merged into one menu.

### Changed

- Ringing in the Talk app is optional and can be turned off in the settings.
- The in-meeting status no longer shows an invented end time; the plugin clears it after the meeting.

## [0.3.0]

### Added

- A Mattermost status "In a Kontur Talk meeting" for the duration of a meeting, restored afterwards unless the user changed it. It can be turned off globally and per user.

### Changed

- Clear messages when an email address is unverified or not found in Talk.
- The *Call in Talk* button in the profile popover is aligned with Mattermost's own buttons.

## [0.2.0]

### Added

- Three visual styles: Default, Cards and Compact, chosen by the administrator and overridable per user.
- The *Calendar with personal keys only* setting.
- Markdown in meeting titles, descriptions and recording posts.

### Changed

- Editing a scheduled meeting updates the waiting room, PIN, SIP and participants.
- New icon set on a single grid and a new bot avatar.

### Security

- Meeting cards are rendered only from posts by the plugin's bot.

## [0.1.0]

### Added

- Meetings, calls, scheduling, recordings and summaries, the sidebar, presence and administrator settings.

[Unreleased]: https://github.com/moonssword/mattermost-plugin-ktalk/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/moonssword/mattermost-plugin-ktalk/releases/tag/v0.5.0
