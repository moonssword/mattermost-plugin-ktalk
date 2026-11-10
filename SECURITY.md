# Security Policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately through [GitHub security advisories](https://github.com/moonssword/mattermost-plugin-ktalk/security/advisories/new). Include the plugin version, the Mattermost version, and steps to reproduce.

You can expect an acknowledgement within a few days. Fixes are released as a new plugin version and noted in the [changelog](CHANGELOG.md).

## Supported versions

Only the latest release receives security fixes.

## Security model

- The space API key is stored as a secret plugin setting and never reaches logs or responses sent to clients. The space address must use HTTPS (plain HTTP is accepted only for a server on the same machine), and the API client does not follow redirects, so the key is never sent to another host.
- Actions of users are made with their own personal Talk keys, so Talk applies each person's rights: creating rooms, running them, planning and answering invitations, and reading recordings. The space key is used for what the plugin does as a whole (webhooks, watching rooms and recordings, matching users, statuses), for ringing in the Talk app and for starting a recording, which Talk refuses to personal keys; a recording is started only for a moderator of the room, as Talk tells through that user's own key. A user without a key may still start a quick meeting or a call, whose room the space key then creates with that user as its moderator.
- Personal Talk keys are encrypted with AES-GCM using a key the plugin generates and keeps in its KV store. A personal key is accepted only when the Mattermost user is matched to a Talk user by verified email and the key's owner carries the same name in Talk; the Talk API does not expose the owner's identifier, so the name is the strongest check available.
- Mattermost users are matched to Talk users by verified email address by default, so an unverified address cannot be used to reach someone else's calendar or recordings. Addresses of SAML, LDAP and OAuth users come from the identity provider and count as verified; the identity provider is trusted with them, as Mattermost itself is when it binds such accounts by email.
- Every action on a meeting is authorised on the server. Buttons in the interface are only a convenience. Rights come from Talk, never from Mattermost roles: running a meeting takes a moderator of its room in Talk, changing or cancelling a scheduled one takes the organizer of its event, answering an invitation takes an attendee of that event, each checked against Talk when the action is made. Channel admins and system admins get nothing extra. Seeing a meeting card takes membership of its channel.
- Recordings, summaries, minutes and transcripts are shown only to Talk users who hold a role on the recording in Talk: its author, the meeting's participants and anyone the owner shared it with. Channel membership and system administrator rights grant nothing. The meeting thread receives notices, never the summary text. By default the plugin does not change a recording's link access in Talk.
- Direct calls follow the server's direct message policy: when messages are limited to team members, a call needs a shared team. The participant search of the scheduling form is limited to teams the user belongs to.
- Meeting cards are rendered only from posts by the plugin's bot; a post of the same type by anyone else is shown as a plain message. Text the plugin writes into Markdown, such as titles from Talk, has its link and emphasis marks escaped.
- Links opened by the interface are restricted to `http` and `https`.
- The webhook endpoint accepts only requests carrying the shared secret header, compared in constant time, and refuses everything while webhooks are turned off. Events only trigger a re-read of the real state from Talk, so a forged or replayed event cannot change a meeting on its own.
- The plugin keeps no state in memory between requests and audits meeting actions through Mattermost's audit log.
