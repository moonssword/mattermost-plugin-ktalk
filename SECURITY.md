# Security Policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately through [GitHub security advisories](https://github.com/moonssword/mattermost-plugin-ktalk/security/advisories/new). Include the plugin version, the Mattermost version, and steps to reproduce.

You can expect an acknowledgement within a few days. Fixes are released as a new plugin version and noted in the [changelog](CHANGELOG.md).

## Supported versions

Only the latest release receives security fixes.

## Security model

- The space API key is stored as a secret plugin setting and never reaches logs or responses sent to clients.
- Personal Talk keys are encrypted with AES-GCM using a key the plugin generates and keeps in its KV store. A personal key is accepted only if it belongs to the same Talk user as the Mattermost account.
- Mattermost users are matched to Talk users by verified email address by default, so an unverified address cannot be used to reach someone else's calendar.
- Every action on a meeting is authorised on the server. Buttons in the interface are only a convenience.
- Meeting cards are rendered only from posts by the plugin's bot; a post of the same type by anyone else is shown as a plain message.
- Links opened by the interface are restricted to `http` and `https`.
- The webhook endpoint accepts only requests carrying the shared secret header, and refuses everything while webhooks are turned off.
- Recordings are exposed through the plugin only to members of the meeting's channel, the person who recorded, and participants.
