# devchat

A small self-hosted chat server for humans and LLMs to talk to each other and
exchange information across sessions — written in Go, backed by SQLite, and
runnable as a Windows service.

## Why

- **Chat log** — every message (from a human, a script, or an LLM) is
  persisted to a local SQLite database, grouped into named "sessions".
- **API for LLMs** — any LLM-based tool or agent can connect to the HTTP/
  WebSocket API, read the conversation, post messages, and read/write a
  small cross-session key/value store to hand off information between
  sessions or agents.
- **Live viewer** — a terminal UI (`devchat-viewer`) lets a human follow a
  session's chat in real time without touching the database or the API
  directly.
- **Runs as a Windows service** — `devchatd` can install itself as a
  background Windows service so it's always available on `localhost`.

## Components

| Binary               | Purpose                                                        |
|-----------------------|-----------------------------------------------------------------|
| `devchatd`            | The server: SQLite storage + HTTP/WebSocket API.                |
| `devchat-viewer`      | Terminal UI that live-follows a session's messages.             |

## Getting started

Requires Go 1.25+.

```sh
go build -o bin/devchatd.exe    ./cmd/devchatd
go build -o bin/devchat-viewer.exe ./cmd/devchat-viewer
```

Run the server in the foreground (handy for local development):

```sh
./bin/devchatd.exe
# devchatd listening on http://127.0.0.1:8990 (db: .\data\devchat.db)
```

In another terminal, follow the chat:

```sh
./bin/devchat-viewer.exe
```

Use `up`/`down` to switch between sessions, `i` to show a ready-to-paste
invite for the selected session, `q` to quit.

### Bringing an LLM into a session

devchat itself never reaches out to an LLM — an LLM only participates once
something (you, an agent framework, a coding assistant with shell access,
...) actually calls the API on its behalf. The easiest way to onboard one:

```sh
curl http://127.0.0.1:8990/api/sessions/<session-id>/invite
```

This returns a plain-text block explaining devchat and showing the exact
`curl` calls to read the transcript, post a message, poll for new messages,
and use the shared key/value store. Paste that text into the LLM's chat or
system prompt (or hand it to an agent that can run shell commands) and it
can start participating right away. The same text is one keypress away in
`devchat-viewer` (`i`).

### Configuration

Both flags and environment variables are supported:

| Flag       | Env var        | Default                          | Description                  |
|------------|----------------|-----------------------------------|-------------------------------|
| `-addr`    | `DEVCHAT_ADDR` | `127.0.0.1:8990`                  | HTTP listen address           |
| `-db`      | `DEVCHAT_DB`   | `<exe dir>\data\devchat.db`       | SQLite database file          |
| `-api`     | `DEVCHAT_API`  | `http://127.0.0.1:8990`           | (viewer only) server URL      |

### Running as a Windows service

`devchatd` wraps [kardianos/service](https://github.com/kardianos/service),
so the same binary can install, control, and run itself as a Windows
service (run an elevated/Administrator shell for `install`/`uninstall`):

```sh
devchatd.exe install     # registers the "DevChat" service
devchatd.exe start
devchatd.exe stop
devchatd.exe uninstall
```

Without a subcommand, `devchatd.exe` (or `devchatd.exe run`) runs in the
foreground — the same entry point the Windows Service Control Manager uses
when the service is started.

## API

There is currently **no authentication** — the API is intended for local or
otherwise trusted network use. Put it behind a reverse proxy or VPN if you
need to expose it more broadly.

`GET /` and `GET /api` return a machine-readable welcome message and a full
list of endpoints, so an LLM client can discover how to use devchat just by
requesting the API root.

### Sessions & messages

| Method & path                              | Description                                             |
|---------------------------------------------|-----------------------------------------------------------|
| `GET /api/sessions`                         | List all sessions.                                        |
| `POST /api/sessions`                        | Create a session: `{"id?": "...", "name?": "..."}`        |
| `GET /api/sessions/{id}`                    | Get a single session.                                     |
| `GET /api/sessions/{id}/messages?since={id}`| List messages after message id `since` (default: all).    |
| `POST /api/sessions/{id}/messages`          | Post a message: `{"role", "author", "content"}`. Creates the session if it doesn't exist yet. |
| `GET /api/sessions/{id}/ws?since={id}`      | WebSocket: replays backlog after `since`, then streams new messages live. |

Every newly created session automatically gets a first message (`role:
system`, `author: devchat`) explaining what devchat is and how to use it, so
any participant reading the transcript — human or LLM — has the instructions
in context.

### Shared key/value store

A small store for exchanging short pieces of information across sessions
(or scoped to a single session).

| Method & path                          | Description                                     |
|------------------------------------------|---------------------------------------------------|
| `GET /api/kv?scope={scope}&key={key}`     | Read a value. `scope` defaults to `global`.       |
| `PUT /api/kv`                             | Write a value: `{"scope?", "key", "value"}`.      |
| `DELETE /api/kv?scope={scope}&key={key}`  | Delete a value.                                   |
| `GET /api/kv/list?scope={scope}`          | List all keys in a scope.                         |

## Project layout

```
cmd/devchatd/         service entry point + Windows service wiring
cmd/devchat-viewer/   terminal UI client
internal/store/       SQLite schema and data access
internal/api/         HTTP/WebSocket handlers
```

## License

No license has been chosen yet; all rights reserved by the author unless a
license file is added.
