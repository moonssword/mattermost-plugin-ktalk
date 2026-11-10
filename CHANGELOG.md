# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.7.0] - 2026-11-10

### Changed

- **Users act in Talk with their own personal keys.** Creating rooms, the personal room, the PIN, guests, the waiting room, *End for everyone*, planning, invitations, the *Recordings* tab and the summary texts all go through the user's personal key, so Talk applies each person's rights. Room moderators are now recognised by Talk's own answer to the user's key. The space key stays for the plugin as a whole, for ringing in the Talk app and for starting a recording, which Talk refuses to personal keys and which the plugin starts only for a room moderator.
- Without a personal key a user can still start a meeting or a call, created with the space key, join meetings and answer calls; everything else needs a key. The *Allow personal API keys* and *Calendar with personal keys only* settings are gone, and planning always needs a personal key.
- Any action that needs a key opens the key form in the sidebar and then offers to continue. A card tells the person who started a meeting without a key that running it needs one, and an invitation can be answered once a key is connected.
- The space key needs only read access to `calendar`.
- A link nobody has opened for two hours leaves the *Now* tab. The room still works and is still watched, so the meeting comes back if someone joins.
- The *Recordings* tab reads Talk's own list of the user's recordings.
- Empty *Now*, *Upcoming* and *Recordings* lists show a mark above the text, as the summary and transcript already did.

### Added

- Until a personal key is connected, the sidebar asks for it at once with the key form open. *Later* folds the form into one line about what a key opens, which can be hidden too.
- The *Connection* block in the System Console shows how many users of the plugin (those who opened the sidebar, started a meeting or connected a key) have connected a personal key.
- Plugin settings inside the sidebar, behind a sliders button: the personal key, then *Appearance*, *Meetings* and *Calls and notices* with lists for choices and switches for yes-or-no settings, and the plugin version at the bottom. They are the same preferences as in **Settings → Контур.Толк** and are saved at once.

### Fixed

- The *Recordings* tab of a busy space showed an empty list with *Show more*: only the first hundred space recordings were checked. Empty pages are now passed over, and *Show more* appears only under recordings.
- Summaries with highlights and minutes appeared as raw JSON. Headlines, topics and tasks are now shown as Talk shows them, and parts hidden in Talk stay hidden.
- The end of a long transcript, summary or settings page could not be scrolled into view in the sidebar.
- The recordings search field used the browser's font instead of the panel's.

## [0.6.0] - 2026-11-10

### Added

- A period filter on the *Recordings* tab, up to a month long and the last 30 days by default. The server refuses longer periods.
- In direct and group messages the call button asks whether to ring or only post a meeting link. In channels it posts the link straight away. `/talk link [topic]` does the same from the keyboard.
- The *In the room* row on a card adds "+N" for connections without a name, such as unnamed guests or a second device.

### Changed

- *Start* in the sidebar posts a meeting link without ringing. In a direct or group chat with calls allowed, an arrow next to it offers *Call*; in channels it stays a single button.
- The repeat of a scheduled meeting is chosen as in Talk's own form: *Weekdays*, *Every day*, *Weekly on <the day of the first meeting>*, *Monthly on <its date>* or *Other…*, which opens the period (days, weeks, months), the interval, the days of the week and the end, on a date or after a number of meetings. A line under the choice says what it comes to, for example "Every two weeks on Mondays and Thursdays until 31.12.2026", and cards and the *Upcoming* tab use the same wording. Meetings planned earlier keep their repeat and open in the new form when edited.
- *Start recording* on a card asks who may open the recording, with Talk's own choices: only me, those present, anyone in the space with the link, anyone on the internet with the link. Talk accepts the access only once a recording is finished, so the plugin applies it then and messages the person who started the recording if Talk refuses. The *Default recording access* setting picks the preselected choice and the access of automatic recordings; recordings started in Talk keep the access chosen there.
- A running recording is marked by a slowly pulsing red dot, which stays still for people who prefer reduced motion.
- Several recordings of one meeting share one *Recordings* button with a menu instead of a button each.
- The period filter on the *Recordings* tab is a single compact field.
- *Back* in the recording view is a quiet button with a chevron instead of a link with an arrow.
- "Still being prepared" and "Not available for this recording" are centred under a light clock or crossed-circle icon.
- A request that does not reach the server, for example while the plugin restarts, is retried quietly and the list on screen stays; "Failed to fetch" is no longer shown.
- The *Current* style is now called *System*, since it follows Mattermost's own look, and the per-user choice *Same as everyone* is now *Default*. Saved settings keep working.

### Fixed

- In a direct or group chat with calls turned off, the call menu said "Link in this channel"; it now says "Link in this chat". The label also follows the plugin settings once they load, instead of keeping what it showed before.
- In the Mattermost desktop app, meetings and recordings opened in a window of the app instead of the device's browser. The plugin no longer opens a blank window first there, so the app hands the address to the system browser.

### Security

- Rights over meetings come from Talk instead of Mattermost roles. Recording, ending for everyone, the PIN, guest access and the waiting room are for the room's moderators in Talk; changing or cancelling a scheduled meeting is for the organizer of its event; answering an invitation is for its attendees in Talk. Channel admins and system admins no longer get any of these, and the meeting's creator gets them only as far as Talk does. Each action asks Talk when it is made, and cards show controls according to the same answer.
- Buttons for a recording on cards and in the meeting thread are shown only to those who may open it in Talk; others no longer see buttons that would refuse them.
- Users who sign in through SAML, LDAP or OAuth are matched to Talk by the address from the identity provider even when Mattermost has not marked it verified. Password accounts still need a verified address.
- Talk is now the only source of access to recordings. The plugin shows a recording, its summary, minutes and transcript only to a Talk user who holds a role on it in Talk: its author, the meeting's participants and anyone the owner shared it with. Channel membership no longer grants access, and system administrators get no exception. Users whose verified address is not found in Talk see no recordings.
- The plugin no longer opens every new recording to the whole space. The *Recording link access* setting became *Default recording access*: the choice preselected when a recording starts from Mattermost and the access of automatic recordings, *Only those present* unless set otherwise. Recordings started in Talk keep the access chosen there.
- The summary is no longer posted as text to the meeting thread, which the whole channel reads. The thread gets a notice with a *Summary* button, and the text opens only for those Talk allows.
- A personal key is refused when the Mattermost user has no matching Talk user, and the owner's name must match in full; before, a key was taken on trust in the first case and a partial name was enough in the second.
- The participant search of the scheduling form checks that the user belongs to the team it searches; the plugin API does not check that by itself.
- Direct calls respect the server's direct message policy: with messages limited to team members, a call to someone in another team is refused instead of opening a channel.
- The space address must use HTTPS, except for a server on the same machine, and the API client no longer follows redirects, so the key cannot leave the space's host.
- Titles and other text written into Markdown by the plugin have link and emphasis marks escaped, so a renamed recording cannot plant a link in the thread.
- Recording grants are cached for a minute and listed in smaller pages, so one request to the *Recordings* tab makes at most a hundred requests to Talk.

## [0.5.1] - 2026-11-09

### Changed

- Test mode opens the plugin only to the listed testers. System administrators no longer get access automatically; the new *Access for system administrators* setting grants it to all of them. The *Connection* block in the System Console warns when nobody has access.

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

[Unreleased]: https://github.com/moonssword/mattermost-plugin-ktalk/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/moonssword/mattermost-plugin-ktalk/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/moonssword/mattermost-plugin-ktalk/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/moonssword/mattermost-plugin-ktalk/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/moonssword/mattermost-plugin-ktalk/releases/tag/v0.5.0
