# turnstile

Small token service for SpotKeeper. It opens the Cloudflare Turnstile page that
the reservation service issued for one booking attempt in a real Camoufox (Firefox) browser under
Xvfb and returns the solved token. One browser worker, no queue: the app asks for
one token at a time. There are no published ports; only the app container reaches it
on the compose network (`http://turnstile:3099`).

## API

### `POST /v2/token`

Request (JSON, at most 8 KiB, both fields required, no extra fields):

    {"turnstileUrl": "https://app.example.com/turnstile?v=5#sid=...&cd=...&tc=...",
     "requestId": "any-correlation-id"}

`turnstileUrl` must be the full URL issued by the reservation service for the current attempt,
including the `#sid=...&cd=...&tc=...` fragment. It must be HTTPS, host exactly
the one set in the `API_DOMAIN` environment variable (required; host only, e.g.
`app.example.com`), path `/turnstile`, with no port, user info, duplicate parameters
or whitespace. The service never builds or signs this data itself.

Responses (`Cache-Control: no-store`):

| Status | Meaning |
| --- | --- |
| 200 | `ok`, `token`, `token_observed_at`, `telemetry_acknowledged`, `request_id`, `context_sha256`, `api_version: 2`, timings |
| 400, 413 | invalid input; the worker is not used |
| 422 | verification failed or telemetry was not acknowledged; no token is returned |
| 500, 504 | worker error or timeout (`CONTEXT_REQUEST_TIMEOUT_S`, default 100) |
| 503 | worker busy (`code: busy`; safe to retry, nothing was started) or `code: not_configured` when `API_DOMAIN` is not set |

After the token is read, the service waits up to 5 s for the service to acknowledge the
`solve_success` telemetry of the same session before closing the page. Full URLs and
tokens are never logged; `context_sha256` identifies the context instead.

### Configuration

| Variable | Description |
| --- | --- |
| `API_DOMAIN` | required: host of the reservation service (no `https://`, no path); the service refuses to start without it |
| `WINDOW_TITLE_MATCH` | optional: part of the browser window title used to find the window; default is the first label of `API_DOMAIN` |

### `GET /health`

200 with `status: ok` when Xvfb and the browser are ready, 503 otherwise. Reports
`api_version`, `workers`, `available` and per-display state.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | 3099 | listen port |
| `WORKERS` | 1 | browser workers |
| `REUSE_BROWSER` | 1 | keep the browser warm between requests |
| `CONTEXT_REQUEST_TIMEOUT_S` | 100 | per-request timeout |

## Platforms

The Dockerfile builds for `linux/amd64` and `linux/arm64`. Camoufox publishes
`lin.x86_64` and `lin.arm64` browser archives on its GitHub releases (checked against
v158.0.1-beta.41) and the `camoufox` pip package maps `aarch64`/`arm64` to them, so
`python -m camoufox fetch` works on both. The arm64 image was not built here; the
claim rests on the release assets and package code. Run it once on an arm64 host
before relying on it.

## Tests

    python3 -m unittest discover -s tests

Needs `flask` and `camoufox` installed. The tests use fake workers and never
touch the real service or Cloudflare.
