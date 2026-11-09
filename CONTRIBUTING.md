# Contributing

Thanks for your interest in the plugin. Bug reports, ideas and pull requests are welcome.

## Before you start

- For anything larger than a small fix, open an issue first so the approach can be agreed on.
- Security problems go through [private advisories](SECURITY.md), not public issues.

## Environment

- **Docker.** The server is built, tested and linted in containers, so a local Go toolchain is not needed.
- **Node.js 22** for the web app.
- **Mattermost 11.7+** for manual testing. The simplest setup is the `mattermost/mattermost-team-edition` image with `MM_SERVICESETTINGS_ENABLELOCALMODE=true`; `make deploy` then installs the plugin with `mmctl --local`. Set `MM_CONTAINER` if your container is not named `mattermost-mattermost-1`.
- **A Kontur Talk space and API key** for checks against the live API. Put them in `.env` following `.env.example`; the file is ignored by git. API reference: [developer.kontur.ru/doc/talk.public.api](https://developer.kontur.ru/doc/talk.public.api).

## Commands

```bash
make test        # go test in Docker, jest
make lint        # golangci-lint in Docker, tsc --noEmit
make bundle      # dist/konturtalk-<version>.tar.gz
make deploy      # build and install into a local Mattermost
make talk-check  # expiry and scopes of the key in .env
```

## Layout

```
server/            Go plugin
  talk/            typed Talk API client, no Mattermost dependencies
  meeting*.go      meeting model and lifecycle
  sync.go          API polling, recordings and summaries
  webhook.go       webhook registration and delivery
  calendar.go      scheduling and invitations
  command.go       /talk and dialogs
  api.go           HTTP routes
webapp/src/        React: cards, sidebar, settings, incoming calls
docs/screenshots/  images used by the README
```

## Conventions

- Code, comments and commit messages are in English. Text shown to users is in Russian; web app strings also get an English entry in `webapp/src/i18n.ts`.
- Everything the plugin renders lives inside `.ktk-root` and takes its colours from Mattermost theme variables. No external fonts or images.
- Icons sit on a 24-unit grid with a 2-unit stroke and plain geometric shapes.
- The server keeps no state in memory between requests, because the plugin may run in a cluster. Mutable state goes through `updateMeeting` in the KV store.
- Fields sent to clients are listed explicitly in `Meeting.view()`. A new field stays server-side until it is added there.
- Every action on a meeting checks permissions on the server.
- Tests and examples use made-up people and addresses (`example.com`, `example.test`), never real accounts.

## Pull requests

- `make test` and `make lint` pass.
- `CHANGELOG.md` has an entry under *Unreleased*.
- The version in `plugin.json` and `webapp/package.json` matches when releasing.
