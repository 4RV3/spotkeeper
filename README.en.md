# SpotKeeper

[Polska wersja](README.md)

## Table of contents

1. [What SpotKeeper is](#what-spotkeeper-is)
2. [Before you start](#before-you-start)
3. [Step 1: Install Docker](#step-1-install-docker)
4. [Step 2: Create a folder and download docker-compose.yml](#step-2-create-a-folder-and-download-docker-composeyml)
5. [Step 3: Fill in the domain, login and password](#step-3-fill-in-the-domain-login-and-password)
6. [Step 4: Start it](#step-4-start-it)
7. [Step 5: Log in to the panel](#step-5-log-in-to-the-panel)
8. [Step 6: Configure in the panel](#step-6-configure-in-the-panel)
9. [Step 7: Check the configuration](#step-7-check-the-configuration)
10. [How it works day to day](#how-it-works-day-to-day)
11. [Troubleshooting](#troubleshooting)
12. [Update, backup, uninstall](#update-backup-uninstall)
13. [Access from the internet](#access-from-the-internet)
14. [All environment variables](#all-environment-variables)
15. [For developers](#for-developers)
16. [License](#license)

The web panel and the in-app help page are in Polish. This guide uses the Polish labels in quotes where you need to click them.

## What SpotKeeper is

SpotKeeper is a self-hosted program that, at the configured reservation opening time, automatically books and pays for a parking spot for you in a parking reservation service (you set its domain in `API_DOMAIN`). It works on a single account in that service. It books whole days, on the weekdays you choose, 28 days ahead.

You control it in a web panel: calendar, booking history, days off, settings.

> **Disclaimer.** SpotKeeper is an **unofficial** client of a parking reservation service. It is not affiliated with or supported by the service operator. Using it **may violate the service's terms**, and your account **may be blocked**. The software comes with no warranty (see [LICENSE](LICENSE)). You use it **at your own risk**.

## Before you start

SpotKeeper books and pays for a spot on your account in the service, the same way you do in its app. So before it can book anything, get this ready:

- [ ] **An account in the reservation service** with a login (e-mail address) and password. If you sign in there with Google or Apple, set a password for the account in the service.
- [ ] **Access to the parking** (usually an invitation from the parking administrator).
- [ ] **A car with a registration number** added in the service's app (Vehicles tab). You will enter the same number in SpotKeeper.
- [ ] **An active payment card** added in the service's app (Payments tab). SpotKeeper pays with the saved card. It does not support BLIK or bank transfers.
- [ ] **The service domain**: just the host name from the service's web address, e.g. `app.example.com` (no `https://`, no path).
- [ ] **A computer or server that is running at the booking opening time** (11:00 by default). Ideally one that runs around the clock. It needs about 2 GB of free RAM for the `turnstile` service and an amd64 or arm64 CPU.

The same hints and frequent questions are in the panel, on the **Pomoc** (Help) page (`/pomoc`, also available without logging in).

## Step 1: Install Docker

SpotKeeper runs in containers, so you need Docker with Docker Compose.

- **Windows and macOS:** install [Docker Desktop](https://www.docker.com/products/docker-desktop/) and start it.
- **Linux:** install Docker Engine following https://docs.docker.com/engine/install/ and the Compose plugin (package `docker-compose-plugin`, see https://docs.docker.com/compose/install/linux/).

Check that it works. Open a terminal (PowerShell, Terminal) and type:

```sh
docker --version
docker compose version
```

You should see two lines with version numbers, e.g. `Docker version 27.x.x` and `Docker Compose version v2.x.x`. If you see "command not found", the installation failed or you need to open a new terminal.

> Instead of Docker you can use Podman with `podman-compose`. The commands are the same (`podman-compose up -d`). After changing the file, use `podman-compose up -d --force-recreate` so the containers are certainly recreated.

## Step 2: Create a folder and download docker-compose.yml

You need one file. Create a separate folder for it and enter it:

```sh
mkdir spotkeeper
cd spotkeeper
```

Download the file using one of these methods.

Linux, macOS, Windows 10/11 (in Windows PowerShell use `curl.exe`):

```sh
curl -O https://raw.githubusercontent.com/4RV3/spotkeeper/main/docker-compose.yml
```

Windows PowerShell:

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/4RV3/spotkeeper/main/docker-compose.yml -OutFile docker-compose.yml
```

Without a terminal: open https://github.com/4RV3/spotkeeper/blob/main/docker-compose.yml, click "Download raw file" (or "Raw" and Save as) and save it in this folder as `docker-compose.yml`.

Check that the file is there:

```sh
ls docker-compose.yml
```

(Windows PowerShell: `dir docker-compose.yml`). Its name should be printed.

## Step 3: Fill in the domain, login and password

Open `docker-compose.yml` in a text editor (Notepad, VS Code, `nano`). You change three values. The domain is right at the top of the file:

```yaml
x-domain: &domain "twoja-domena.pl"   # <- wpisz domenę serwisu rezerwacji (bez https://)
```

The login and password are under `spotkeeper:` → `environment:`:

```yaml
      ACCOUNT_LOGIN: "twoj@email.pl"    # <- wpisz
      ACCOUNT_PASSWORD: "twoje-haslo"   # <- wpisz
```

(The comments in the file are in Polish: "wpisz" means "enter".) After editing it looks like this (example values, enter your own):

```yaml
x-domain: &domain "app.example.com"

      ACCOUNT_LOGIN: "jan.kowalski@example.com"
      ACCOUNT_PASSWORD: "MyPassword123"
```

Rules:

- The domain is just the host name: no `https://`, no trailing `/`, no path.
- The login must be an e-mail address.
- Keep the values in quotes.
- If the password contains a `$`, write it as `$$` (otherwise Compose treats it as a variable). For example, the password `ab$cd` becomes `"ab$$cd"`.
- A password with a `"` inside needs an escaped `\"`, or change the password in the service.
- **Optionally** change `APP_PASSWORD` (the password for the SpotKeeper panel, `spotkeeper` by default, at least 8 characters). Definitely do it if the panel will be reachable from the internet.

If you leave `twoja-domena.pl`, `twoj@email.pl` or `twoje-haslo`, the app **will not start**, and the logs (`docker compose logs spotkeeper`) will say the placeholder values from `docker-compose.yml` are still there. The same happens with an empty value, a domain that includes `https://`, or a login that is not an e-mail address.

## Step 4: Start it

In the folder with the file run:

```sh
docker compose up -d
```

On the first start Docker downloads two images (the app is small, but the browser image for the `turnstile` service is large). It can take a few minutes. When it finishes, check the state:

```sh
docker compose ps
```

Expected result: two services, `spotkeeper` and `turnstile`, both `running` (`Up`), with `(healthy)` next to `turnstile`. Right after the start `turnstile` may show `(health: starting)`. Wait a minute (it has 60 s to warm up) and check again. The `spotkeeper` app starts only when `turnstile` is healthy.

Open **http://localhost:8080** in a browser (if SpotKeeper runs on another computer: `http://<server-IP-address>:8080`). You should see the "Zaloguj się" (Log in) screen.

If something does not work, see [Troubleshooting](#troubleshooting).

## Step 5: Log in to the panel

Enter the panel password: the `APP_PASSWORD` value from the file. If you did not change it, it is `spotkeeper`. **Change it** (in `docker-compose.yml`, then `docker compose up -d`), especially if the panel is reachable outside your network. After 5 wrong attempts, logging in is blocked for 15 minutes. A logged-in session lasts 30 days.

After logging in you will see the status, the calendar, the booking history and the **Ustawienia** (Settings) card.

## Step 6: Configure in the panel

In the **Ustawienia** card fill in, in order:

1. **Parking.** Pick a parking from the list (it is fetched from the service; you only see parkings your account can access). If the list does not load, expand **Zaawansowane** (Advanced) and enter the parking identifier (GUID, 36 characters) in "Identyfikator parkingu (ręcznie)". A manually entered one takes precedence over the list.
2. **Booking opening time** ("Godzina otwarcia rezerwacji", HH:MM, Polish time, 11:00 by default). This is when the service opens booking for a new day. Check it in the service's app: see when spots for the day 28 days ahead appear. Enter exactly that time, because SpotKeeper does not read it from the service.
3. **Payment amount** ("Kwota płatności", PLN, default 15). It must match the spot price at your parking. SpotKeeper does not read the price from the service; it pays the amount you enter with the saved card. Allowed: 0.01 to 1000 (decimal point or comma).
4. **Registration number.** The same as the car in the service's app (spaces and letter case do not matter).
5. **Level.** "dowolny" (any) or a specific level of the parking. The level list is fetched from the day with the most free spots. If nothing is free at the moment, the list can be empty. A previously saved level is then kept. The level works as a text filter (case-insensitive). Handicapped, charger and mini-EV spots are always skipped. Among the matching free spots, SpotKeeper picks one at random.
6. **Booking days.** Tick the weekdays **for which** you want a spot. The day that gets booked is always 28 days after the day the automation runs (e.g. on a Monday the booking opens the day 28 days ahead, which is also a Monday). If you tick nothing, nothing will be booked.
7. **Active.** The automation switch. It is on after the first start.
8. **Skip holidays** ("Pomijaj święta"). When on, the automation does not book on Polish public holidays.
9. **Days off.** In the calendar click "+ urlop" on a day to skip it (click "× urlop" to remove it). The button appears only on days the automation would book and whose booking has not opened yet (up to a year ahead).
10. **Advanced.** The manual parking identifier (see above) and the payment **IP address**. Empty means automatic detection (the service from `PUBLIC_IP_URL`, api.ipify.org by default). Enter an IPv4 address only if you know it.

Click **Zapisz** (Save). "Zapisano ustawienia." appears at the top. Changes apply immediately, no restart needed. On an error (e.g. a bad time format or GUID) you get a message and nothing is saved.

## Step 7: Check the configuration

Click **Sprawdź konfigurację** (page `/check`), then **Sprawdź**. The check never books or pays. It runs six steps in order and stops at the first failure (the rest are then "Pominięto", skipped):

| Step | What it checks | What to do on ✗ |
| --- | --- | --- |
| Ustawienia (Settings) | that the registration number and parking are set; it also shows the payment IP | fill in the missing field and click Save |
| Logowanie do serwisu (Service login) | that `ACCOUNT_LOGIN` and `ACCOUNT_PASSWORD` work | fix the login, password or `API_DOMAIN` in `docker-compose.yml`, then `docker compose up -d` |
| Pojazd i karta (Vehicle and card) | that the service has a car with this number and an active card | add the car or card in the service's app, or fix the number in Settings |
| Parking | that the parking responds | check the parking and that the account has access to it |
| Poziom (Level) | that the chosen level exists at the parking | pick another level or "dowolny" (when no spots are free, the step passes with a warning) |
| Turnstile | that the `turnstile` service returns a token | check `docker compose ps` and `docker compose logs turnstile` (see [Troubleshooting](#troubleshooting)) |

When all steps show ✓, "Konfiguracja poprawna" (configuration correct) appears. You are done: SpotKeeper will book on its own.

## How it works day to day

1. Every day, about 10 minutes before the opening time (`PREPARE_LEAD`), SpotKeeper logs in to the service and prepares the session.
2. The target day is the day 28 days later. If it falls on a ticked weekday, is not a day off and (with "Skip holidays" on) not a holiday, the program books.
3. Exactly at the opening time it asks the service for free spots, picks one (skipping handicapped, charger and mini-EV spots; matching the chosen level), solves the Turnstile check through the `turnstile` service and books. It makes up to 10 attempts, at most 7.5 minutes in total. If the service answers twice that you already have a booking for that time, the day gets the status "pominięto" (skipped).
4. After a successful booking it pays the amount you entered with the saved card (up to 5 tries to start the payment) and checks the status every 2 seconds, up to 20 times.

In the calendar and in the history (the "Ostatnie rezerwacje" table with columns: day, type, status, attempts, spot, details) you will see a status:

| Status | Meaning |
| --- | --- |
| oczekuje | scheduled, not started yet |
| w toku | booking in progress |
| opłacono | spot booked and paid |
| niepotwierdzona (płatność) | spot booked, but the service did not confirm the payment, or it could not be started |
| nieudana | booking failed; the reason is in the Details column |
| anulowana | the task was interrupted (e.g. the app was stopped) |
| pominięto | day off, holiday, or you already have a booking for that day |

**Unconfirmed payment.** SpotKeeper checks it again after 5 and after 30 minutes (also after an app restart). If the payment went through, the status changes to "opłacono". If not, the details show "płatność nadal niepotwierdzona po 30 min — sprawdź w aplikacji serwisu" (payment still unconfirmed after 30 min, check in the service's app). Then open the service's app and check (pay) the booking manually.

The calendar icons are explained under **Legenda** (Legend) below the calendar.

## Troubleshooting

Logs: `docker compose logs -f spotkeeper` and `docker compose logs -f turnstile` (leave with Ctrl+C). Change the verbosity with `LOG_LEVEL`.

| Symptom | Cause | Fix |
| --- | --- | --- |
| `docker compose up -d` reports a syntax error | bad indentation or quote in `docker-compose.yml` | fix the file; validate it with `docker compose config -q` |
| `spotkeeper` keeps restarting, logs say "config: ..." | placeholder values or a wrong domain, login, password, `APP_PASSWORD` shorter than 8 characters | fix the file as in [Step 3](#step-3-fill-in-the-domain-login-and-password), then `docker compose up -d` |
| http://localhost:8080 does not open | the container is not running or port 8080 is taken | `docker compose ps`; if the port is taken, change `"8080:8080"` in the file to e.g. `"8081:8080"` |
| `turnstile` stays `unhealthy` or `starting` | too little memory, first warm-up, browser error | wait 1-2 minutes; `docker compose logs turnstile`; provide about 2 GB of RAM; on arm64 see [turnstile/README.md](turnstile/README.md) |
| "Zaloguj się" rejects the password | that is the panel password (`APP_PASSWORD`), not the service one; after 5 failures a 15 min block | enter the value from the file; wait 15 minutes |
| The panel logged you out after changing `APP_PASSWORD` | changing it invalidates all sessions | log in with the new password |
| Parking list empty or "Nie udało się pobrać listy parkingów" | wrong login/password/domain or the service is not responding | run the configuration check; if needed enter the GUID manually (Advanced) |
| Level list empty | no free spots on any day | normal; the saved level is kept, or choose "dowolny" |
| Check: ✗ Logowanie | wrong login/password or domain | fix `ACCOUNT_LOGIN`, `ACCOUNT_PASSWORD`, `API_DOMAIN`; `docker compose up -d` |
| Check: ✗ Pojazd i karta | no car with that number or no active card | add them in the service's app or fix the number in Settings |
| Check: ✗ Parking | wrong parking or no access | pick the parking from the list; ask the administrator for access |
| Check: ✗ Turnstile | `turnstile` is not running or `TURNSTILE_URL` is wrong | `docker compose ps`, `docker compose logs turnstile` |
| Nothing is booked at the opening time | "Aktywne" is off, no days ticked, wrong time, day off/holiday, computer off | check the status card ("Następna rezerwacja automatyczna"), Settings and `docker compose ps` |
| Status "nieudana", details "Reservation failed after 10 attempts" or "budget exhausted" | there were no free spots (matching the level) during the attempts | consider level "dowolny"; check the opening time |
| Status "nieudana", "Context verification failed: ..." | the `turnstile` service did not deliver a token | `docker compose logs turnstile` |
| Status "niepotwierdzona" | the service did not confirm the payment | wait for the checks after 5 and 30 min; then check in the service's app; make sure the amount matches the spot price |
| Status "pominięto" | day off, holiday, or you already have a booking for that day | nothing, it is not an error |

## Update, backup, uninstall

**Update** (in the folder with `docker-compose.yml`):

```sh
docker compose pull && docker compose up -d
```

Settings and history stay in the data volume.

**Backup.** The database (`spotkeeper.db`) and the session key (`session.key`) live in the `spotkeeper-data` volume (`/data` in the container). Find the full volume name (usually `<folder-name>_spotkeeper-data`):

```sh
docker volume ls
```

Make a backup into a file in the current folder (substitute the volume name; ideally stop the app first: `docker compose stop spotkeeper`):

```sh
docker run --rm -v spotkeeper_spotkeeper-data:/data:ro -v "$PWD":/backup alpine tar czf /backup/spotkeeper-backup.tgz -C /data .
```

(Windows PowerShell: use `${PWD}` instead of `"$PWD"`.) Then start the app again: `docker compose start spotkeeper`.

**Changing the panel password.** Change `APP_PASSWORD` in the file and run `docker compose up -d`. The session secret is derived from `session.key` and `APP_PASSWORD`, so **everyone is logged out**. Deleting `session.key` has the same effect.

**Changing the service login, password or domain.** Edit the file and run `docker compose up -d`.

**Stopping** (data is kept): `docker compose down`.

**Uninstalling together with the data** (removes settings and history!):

```sh
docker compose down -v
```

Then you can delete the folder and the images (`docker image rm ghcr.io/4rv3/spotkeeper ghcr.io/4rv3/spotkeeper-turnstile`).

## Access from the internet

By default the panel is served over plain HTTP on port 8080. **Do not expose it to the internet like that.** If you must, only behind a reverse proxy with HTTPS and a strong `APP_PASSWORD`. Do not publish the `turnstile` service port (the compose file does not).

In `docker-compose.yml` uncomment and set:

```yaml
      COOKIE_SECURE: "true"
      TRUSTED_PROXIES: "172.18.0.1"   # the address the proxy connects from
```

- `COOKIE_SECURE: "true"` marks the session cookie as HTTPS-only. Set it only once you really open the panel over HTTPS, otherwise you cannot log in.
- `TRUSTED_PROXIES` is a comma-separated list of IP addresses or CIDR networks allowed to set the `X-Forwarded-For` header. That way the failed-login limit applies to the real client address. The proxy address depends on your network (with the proxy on the same host it is usually the Docker network gateway; check with `docker network inspect`). The example above is only an illustration.

Example with [Caddy](https://caddyserver.com/) on the same server (HTTPS with a Let's Encrypt certificate, automatically). The `Caddyfile`:

```
spotkeeper.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

To keep port 8080 invisible from outside, change `"8080:8080"` in `docker-compose.yml` to `"127.0.0.1:8080:8080"`.

## All environment variables

You set them in the `environment:` section of the `spotkeeper` service in `docker-compose.yml` (the advanced ones are commented out there with their defaults; uncomment to change). After a change run `docker compose up -d`. Durations use Go format, e.g. `500ms`, `30s`, `10m`.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `API_DOMAIN` | yes | none (the file ships `twoja-domena.pl` — you must change it) | domain of the reservation service, host name only (lower case, no `https://` or path); the API address and the allowed turnstile URL prefix are derived from it. In compose it is set once at the top of the file and also passed to `turnstile` |
| `ACCOUNT_LOGIN` | yes | none (the file ships `twoj@email.pl` — you must change it) | service account login; must be an e-mail address (stored in lower case) |
| `ACCOUNT_PASSWORD` | yes | none (the file ships `twoje-haslo` — you must change it) | service account password |
| `APP_PASSWORD` | no | `spotkeeper` | panel password, at least 8 characters; with the default the log shows a `default_app_password` warning |
| `TZ` | no | `Europe/Warsaw` (in compose) | container time zone. Times in the panel are always computed in Polish time |
| `LISTEN_ADDR` | no | `:8080` | app listen address |
| `DB_PATH` | no | `/data/spotkeeper.db` | SQLite database path |
| `COOKIE_SECURE` | no | `false` | `true` when the panel is behind HTTPS |
| `TRUSTED_PROXIES` | no | empty | IP addresses or CIDRs of trusted proxies (comma-separated) allowed to set `X-Forwarded-For` |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error` |
| `API_BASE` | no | `https://<API_DOMAIN>/api` | override of the API address (used by the demo and tests) |
| `PUBLIC_IP_URL` | no | `https://api.ipify.org` | service returning the public IP address as text; used when the IP field in Settings is empty. Set it empty (`PUBLIC_IP_URL: ""`) or `off` to disable detection |
| `TURNSTILE_URL` | no | `http://turnstile:3099` | address of the turnstile service |
| `POLL_INTERVAL` | no | `500ms` | how often to poll for free spots |
| `PREPARE_LEAD` | no | `10m` | how long before the opening time to prepare the session |
| `MAX_TOKEN_AGE` | no | `30s` | maximum age of a turnstile token |
| `APP_VERSION` | no | `4.7.1` | app version sent to the service API (change only if the service requires a newer one) |

The `turnstile` service has its own variables (among others `PORT`, `WORKERS`, `CONTEXT_REQUEST_TIMEOUT_S`); they are described in [turnstile/README.md](turnstile/README.md). You usually do not need to change them.

The opening time and the payment amount are not environment variables: you set them in the panel.

## For developers

By default `docker-compose.yml` pulls ready-made images from GHCR (`ghcr.io/4rv3/spotkeeper` and `ghcr.io/4rv3/spotkeeper-turnstile`). After cloning the repository you can build them locally with the `docker-compose.build.yml` override (images are built from the `app/` and `turnstile/` directories):

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

Tests:

```sh
cd app && go test ./...                       # the app (Go)
cd turnstile && python3 -m unittest discover -s tests   # the turnstile service (needs flask and camoufox)
```

CI (GitHub Actions, `.github/workflows/ci.yml`) checks `gofmt`, `go vet`, `go test -race`, the turnstile tests and the compose files on every push and pull request.

**Releases.** Pushing a `v*` tag (e.g. `v1.2.3`) runs the Release workflow, which publishes the `spotkeeper` and `spotkeeper-turnstile` images (amd64 and arm64) to GHCR with the version tag and `latest`.

**arm64 note.** The `turnstile` image relies on Camoufox archives for arm64, but it has not been built or tested on that architecture yet. Run it on an arm64 host first (details in [turnstile/README.md](turnstile/README.md)).

## License

MIT, see [LICENSE](LICENSE).

Logo and icons: the emoji 👹 (U+1F479) from [Noto Emoji](https://github.com/googlefonts/noto-emoji), Apache 2.0 license, see [NOTICE](app/internal/web/static/NOTICE-noto-emoji.txt).
