# Signal Scheduler Bot

`signal-scheduler-bot` is a small Go service that lets one Signal user schedule outgoing messages by sending commands to their own `Note to Self` chat.

It uses:

- `signal-rest-api` to receive commands, resolve contacts, and send messages
- `bbolt` to store scheduled messages and delivery state

## Goal

The goal of this project is to keep scheduled Signal messaging simple:

- one user
- one Signal account
- one command channel: `Note to Self`
- one local database
- no extra control panel

## Why This Exists

Signal already supports scheduled messages on Android. However, Signal's own support documentation says those messages are locally queued on your own device, not scheduled by the Signal service for you.

This project exists for cases where:

- you do not want delayed delivery to depend on a specific Android device
- you do not have an Android device available for scheduling
- you want scheduling to run in a small always-on service instead of your phone

## What It Is

- A lightweight personal scheduling bot for Signal
- A background worker that polls your `Note to Self` chat for commands
- A scheduler that sends due messages later
- A small service that stores state in a local BoltDB file

## What It Is Not

- Not a multi-user bot
- Not a generic Signal automation platform
- Not a replacement for `signal-rest-api`
- Not a web application or admin panel
- Not a workflow engine or enterprise scheduler
- Not a group messaging management tool

## How It Works

The service runs two independent workers:

1. The command worker polls incoming messages from your `Note to Self` chat.
2. It parses supported commands such as `/schedule`, `/upcoming`, `/cancel`, and `/help`.
3. When you schedule a message, the bot resolves the recipient through `signal-rest-api` and stores the scheduled message in BoltDB.
4. The scheduler worker periodically scans stored messages and sends the ones that are due.
5. If delivery fails temporarily, the message is retried.
6. If delivery fails permanently, the message is marked as failed and the bot sends a failure notification back to `Note to Self`.

All timestamps are stored in UTC. User-facing date parsing is done in the configured timezone.

## Requirements

- Docker
- Docker Compose
- A Signal account on your phone

## Quick Start

1. Copy the example environment file:

```bash
cp .env.example .env
```

2. Edit `.env` and set your Signal number in international format:

```env
SIGNAL_ACCOUNT=+380501234567
```

3. Start `signal-rest-api` first:

```bash
docker compose up -d signal-api
```

4. Link `signal-rest-api` to your Signal account.

5. Start the scheduler:

```bash
docker compose up -d scheduler
```

6. Open Signal on your phone and send `/help` to `Note to Self`.

## Linking `signal-rest-api` to Your Signal Account

This project is designed around linking `signal-rest-api` as a secondary device to the same Signal account you already use on your phone.

With the provided [`docker-compose.yml`](/Users/demeero/workspace/signal-scheduler-bot/docker-compose.yml:1), the Signal REST API is exposed on `http://localhost:18080`.

### Recommended setup: link as a secondary device

1. Start the `signal-api` service:

```bash
docker compose up -d signal-api
```

2. Open the QR code link in your browser:

```text
http://localhost:18080/v1/qrcodelink?device_name=signal-scheduler-bot
```

3. On your phone, open Signal and go to:

```text
Settings -> Linked devices -> +
```

4. Scan the QR code shown by `signal-rest-api`.

5. Wait until the device is linked.

6. Make sure `.env` contains the same Signal phone number as the linked account:

```env
SIGNAL_ACCOUNT=+380501234567
```

7. Start the scheduler service:

```bash
docker compose up -d scheduler
```

### Notes

- The `signal-api` container stores Signal state under `./var/signal-rest-api`.
- Keep that directory if you want the linked device to survive container recreation.
- The scheduler container stores its BoltDB file under `./var`.

## Using the Bot

The bot only reacts to commands sent to your own `Note to Self` chat.

### Supported commands

```text
/schedule YYYY-MM-DD HH:mm +380XXXXXXXXX Message text
/schedule tomorrow HH:mm +380XXXXXXXXX Message text
/schedule today HH:mm +380XXXXXXXXX Message text

/schedule YYYY-MM-DD HH:mm "Contact Name" Message text
/schedule tomorrow HH:mm "Contact Name" Message text
/schedule today HH:mm "Contact Name" Message text

/upcoming
/cancel MESSAGE_ID
/help
```

### Examples

Schedule by phone number:

```text
/schedule 2026-07-20 09:00 +380501112233 Good morning
```

Schedule by contact name:

```text
/schedule tomorrow 18:30 "Alice Smith" Dinner starts in 30 minutes
```

List upcoming messages:

```text
/upcoming
```

Cancel a scheduled message:

```text
/cancel 42
```

### Command behavior

- Parsing is strict.
- Invalid commands produce an error reply in `Note to Self`.
- Recipients must already exist in Signal contacts.
- Contact names may be quoted with regular double quotes or typographic quotes.
- The bot stores both the original recipient text and the resolved recipient identifier.

## Configuration

The service reads configuration from environment variables.

`docker-compose.yml` overrides `SIGNAL_API_BASE_URL` and `BOLT_PATH` for the `scheduler` service, so the table below describes application defaults, not necessarily the final values inside the compose stack.

| name | description | default |
| --- | --- | --- |
| `SIGNAL_ACCOUNT` | Signal account used by the bot. Must be set to your number in international format. | required |
| `SIGNAL_API_BASE_URL` | Base URL of `signal-rest-api`. | `http://localhost:18080` |
| `TIMEZONE` | Timezone used to parse `today`, `tomorrow`, and displayed times. | `Europe/Kyiv` |
| `LOG_CONFIG` | Log parsed configuration on startup. | `true` |
| `LOG_LEVEL` | Log level. | `debug` |
| `LOG_ADD_SOURCE` | Include source file and line in logs. | `true` |
| `LOG_JSON` | Emit logs as JSON. | `false` |
| `LOG_PRETTY` | Emit human-friendly pretty logs. | `true` |
| `BOLT_PATH` | Path to the BoltDB database file. | `./var/signal-scheduler-bot` |
| `BOLT_TIMEOUT` | How long BoltDB waits for a writable lock. | `5s` |
| `SIGNAL_REQUEST_TIMEOUT` | Timeout for a single request to `signal-rest-api`. | `30s` |
| `BOT_POLL_INTERVAL` | How often the command worker polls `Note to Self`. | `5s` |
| `OUTBOX_WORKER_INTERVAL` | How often the scheduler scans for due messages. | `1s` |
| `OUTBOX_VACUUM_INTERVAL` | How often old terminal messages are cleaned up. | `1h` |
| `OUTBOX_MAX_ATTEMPTS` | Maximum number of delivery attempts for a scheduled message. | `5` |
| `OUTBOX_MAX_AGE` | Maximum allowed delay after scheduled time before a due message is discarded as expired. | `15m` |
| `OUTBOX_VACUUM_RETENTION` | How long sent, failed, and cancelled messages are kept before deletion. | `720h` |

## Docker Compose Layout

The provided compose stack contains two services:

- `signal-api`: the external Signal bridge based on `bbernhard/signal-cli-rest-api`
- `scheduler`: this project

Start everything:

```bash
docker compose up -d
```

Stop everything:

```bash
docker compose down
```

## Development

Run locally:

```bash
task run
```

Run tests:

```bash
task test
```

Build the binary:

```bash
task build
```

## Operational Notes

- The bot sends confirmation and error messages back to your own Signal account.
- Scheduled messages are identified by increasing numeric IDs.
- `Note to Self` replies are also stored in the same outbox flow.
- If you recreate containers, keep the `./var` directory to preserve linked-device state and scheduled messages.

## Related Projects

- `signal-rest-api`: [bbernhard/signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api)
- Signal REST API documentation: [bbernhard.github.io/signal-cli-rest-api](https://bbernhard.github.io/signal-cli-rest-api/)
