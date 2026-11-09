# SpotKeeper

[Polska wersja](README.md) · [Full guide](docs/GUIDE.en.md)

SpotKeeper is a self-hosted program that, at the reservation opening time, automatically books and pays for a parking spot on your account in a parking reservation service. It books whole days, on the weekdays you choose, 28 days ahead. You control it in a web panel (in Polish).

> **Disclaimer.** SpotKeeper is an **unofficial** client of a reservation service. It is not affiliated with the service operator. Using it **may violate the service's terms**, and your account **may be blocked**. No warranty (see [LICENSE](LICENSE)). You use it **at your own risk**.

## Quick start

**You need:** Docker with Compose (about 2 GB of RAM), an account in the service with a login (e-mail) and password, a car and an active card added in the service's app, and the service's domain (host only, e.g. `app.example.com`).

**1. Create a `spotkeeper` folder and save this as `docker-compose.yml` in it.** Change only the marked values:

```yaml
x-domain: &domain "app.example.com"     # <- service domain (host only, no https://)

services:
  spotkeeper:
    image: ghcr.io/4rv3/spotkeeper:latest
    environment:
      API_DOMAIN: *domain
      ACCOUNT_LOGIN: "jan.kowalski@example.com"  # <- your service login (e-mail)
      ACCOUNT_PASSWORD: "YourPassword"           # <- your service password (write $ as $$)
      APP_PASSWORD: "change-me-123"              # <- SpotKeeper panel password (min. 8 characters)
      TZ: Europe/Warsaw
    ports:
      - "8080:8080"
    volumes:
      - spotkeeper-data:/data
    depends_on:
      turnstile:
        condition: service_healthy
    restart: unless-stopped

  turnstile:
    image: ghcr.io/4rv3/spotkeeper-turnstile:latest
    environment:
      API_DOMAIN: *domain
    shm_size: 2g
    healthcheck:
      test: ["CMD-SHELL", "python3 -c \"import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:3099/health', timeout=5).status == 200 else 1)\""]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 60s
    restart: unless-stopped

volumes:
  spotkeeper-data:
```

The commented file with every option: [docker-compose.yml](docker-compose.yml).

**2. Start it** in that folder:

```sh
docker compose up -d
docker compose ps        # both services "Up", turnstile "(healthy)" after about a minute
```

**3. Open http://localhost:8080**, log in with the `APP_PASSWORD` and in **Ustawienia** (Settings) pick the parking, the reservation opening time, the registration number and the weekdays. Click **Zapisz** (Save), then **Sprawdź konfigurację** (Check configuration) to test the setup.

Done. To update: `docker compose pull && docker compose up -d`.

## Full guide

Everything step by step, with troubleshooting, backup, internet access and all environment variables: **[docs/GUIDE.en.md](docs/GUIDE.en.md)** (Polski: [docs/INSTRUKCJA.md](docs/INSTRUKCJA.md)).

## License

MIT, see [LICENSE](LICENSE).
