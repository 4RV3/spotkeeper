# SpotKeeper

[English version](README.en.md) · [Pełna instrukcja](docs/INSTRUKCJA.md)

SpotKeeper to samodzielnie hostowany program, który o godzinie otwarcia rezerwacji sam rezerwuje i opłaca miejsce parkingowe na Twoim koncie w serwisie rezerwacji. Rezerwuje całe dni, w wybrane dni tygodnia, 28 dni naprzód. Obsługujesz go w panelu WWW.

> **Zastrzeżenie.** SpotKeeper jest **nieoficjalnym** klientem serwisu rezerwacji. Nie jest powiązany z operatorem serwisu. Jego użycie **może naruszać regulamin serwisu**, a Twoje konto **może zostać zablokowane**. Program działa bez gwarancji (patrz [LICENSE](LICENSE)). Używasz go **na własną odpowiedzialność**.

## Szybki start

**Potrzebujesz:** Dockera z Compose (ok. 2 GB RAM), konta w serwisie z loginem (e-mail) i hasłem, samochodu i aktywnej karty dodanych w aplikacji serwisu oraz domeny serwisu (sam host, np. `app.example.com`).

**1. Utwórz folder `spotkeeper` i zapisz w nim plik `docker-compose.yml`.** Zmień tylko oznaczone wartości:

```yaml
x-domain: &domain "app.example.com"     # <- domena serwisu (sam host, bez https://)

services:
  spotkeeper:
    image: ghcr.io/4rv3/spotkeeper:latest
    environment:
      API_DOMAIN: *domain
      ACCOUNT_LOGIN: "jan.kowalski@example.com"  # <- Twój login w serwisie (e-mail)
      ACCOUNT_PASSWORD: "TwojeHaslo"             # <- Twoje hasło w serwisie ($ zapisz jako $$)
      APP_PASSWORD: "zmien-mnie-123"             # <- hasło do panelu SpotKeepera (min. 8 znaków)
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

Plik z komentarzami i wszystkimi opcjami: [docker-compose.yml](docker-compose.yml).

**2. Uruchom** w tym folderze:

```sh
docker compose up -d
docker compose ps        # obie usługi "Up", turnstile "(healthy)" po ok. minucie
```

**3. Otwórz http://localhost:8080**, zaloguj się hasłem z `APP_PASSWORD` i w karcie **Ustawienia** wybierz parking, godzinę otwarcia rezerwacji, numer rejestracyjny i dni tygodnia. Kliknij **Zapisz**, a potem **Sprawdź konfigurację**, żeby wszystko przetestować.

Gotowe. Aktualizacja: `docker compose pull && docker compose up -d`.

## Pełna instrukcja

Wszystko krok po kroku, wraz z rozwiązywaniem problemów, kopią zapasową, dostępem z internetu i listą zmiennych środowiskowych: **[docs/INSTRUKCJA.md](docs/INSTRUKCJA.md)** (English: [docs/GUIDE.en.md](docs/GUIDE.en.md)).

## Licencja

MIT, patrz [LICENSE](LICENSE).
