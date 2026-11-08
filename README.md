# SpotKeeper

[English version](README.en.md)

## Spis treści

1. [Czym jest SpotKeeper](#czym-jest-spotkeeper)
2. [Zanim zaczniesz](#zanim-zaczniesz)
3. [Krok 1: Zainstaluj Dockera](#krok-1-zainstaluj-dockera)
4. [Krok 2: Utwórz folder i pobierz docker-compose.yml](#krok-2-utwórz-folder-i-pobierz-docker-composeyml)
5. [Krok 3: Uzupełnij domenę, login i hasło](#krok-3-uzupełnij-domenę-login-i-hasło)
6. [Krok 4: Uruchom](#krok-4-uruchom)
7. [Krok 5: Zaloguj się do panelu](#krok-5-zaloguj-się-do-panelu)
8. [Krok 6: Skonfiguruj w panelu](#krok-6-skonfiguruj-w-panelu)
9. [Krok 7: Sprawdź konfigurację](#krok-7-sprawdź-konfigurację)
10. [Jak to działa na co dzień](#jak-to-działa-na-co-dzień)
11. [Rozwiązywanie problemów](#rozwiązywanie-problemów)
12. [Aktualizacja, kopia zapasowa, odinstalowanie](#aktualizacja-kopia-zapasowa-odinstalowanie)
13. [Dostęp z internetu](#dostęp-z-internetu)
14. [Wszystkie zmienne środowiskowe](#wszystkie-zmienne-środowiskowe)
15. [Dla programistów](#dla-programistów)
16. [Licencja](#licencja)

## Czym jest SpotKeeper

SpotKeeper to samodzielnie hostowany program, który o ustawionej godzinie otwarcia rezerwacji sam rezerwuje i opłaca dla Ciebie miejsce parkingowe w serwisie rezerwacji (jego domenę wpisujesz w `API_DOMAIN`, sugerowana to app.pa****sh.io). Działa na jednym koncie w tym serwisie. Rezerwuje całe dni, w wybrane dni tygodnia, z wyprzedzeniem 28 dni.

Obsługujesz go w panelu WWW: kalendarz, historia rezerwacji, urlopy, ustawienia.

> **Zastrzeżenie.** SpotKeeper jest **nieoficjalnym** klientem serwisu rezerwacji miejsc parkingowych. Nie jest powiązany z operatorem serwisu ani przez niego wspierany. Jego użycie **może naruszać regulamin serwisu**, a Twoje konto **może zostać zablokowane**. Program działa bez żadnej gwarancji (patrz [LICENSE](LICENSE)). Używasz go **na własną odpowiedzialność**.

## Zanim zaczniesz

SpotKeeper rezerwuje i opłaca miejsce na Twoim koncie w serwisie, tak jak Ty robisz to w jego aplikacji. Dlatego zanim cokolwiek zarezerwuje, przygotuj:

- [ ] **Konto w serwisie rezerwacji** z loginem (adres e-mail) i hasłem. Jeśli logujesz się tam przez Google lub Apple, ustaw w serwisie hasło do konta.
- [ ] **Dostęp do parkingu** (zwykle zaproszenie od administratora parkingu).
- [ ] **Samochód z numerem rejestracyjnym** dodany w aplikacji serwisu (zakładka Pojazdy). Ten sam numer wpiszesz w SpotKeeperze.
- [ ] **Aktywną kartę płatniczą** dodaną w aplikacji serwisu (zakładka Płatności). SpotKeeper płaci zapisaną kartą. Nie obsługuje BLIK ani przelewów.
- [ ] **Domenę serwisu**: sama nazwa hosta z adresu strony serwisu, np. `app.example.com` (bez `https://` i bez ścieżki).
- [ ] **Komputer lub serwer, który działa o godzinie otwarcia rezerwacji** (domyślnie 11:00). Najlepiej taki, który pracuje non stop. Potrzebuje ok. 2 GB wolnej pamięci RAM dla usługi `turnstile` i architektury amd64 lub arm64.

Te same wskazówki i najczęstsze pytania są w panelu, na stronie **Pomoc** (`/pomoc`, dostępna też bez logowania).

## Krok 1: Zainstaluj Dockera

SpotKeeper działa w kontenerach, więc potrzebujesz Dockera z Docker Compose.

- **Windows i macOS:** zainstaluj [Docker Desktop](https://www.docker.com/products/docker-desktop/) i go uruchom.
- **Linux:** zainstaluj Docker Engine według instrukcji na https://docs.docker.com/engine/install/ i dodatek Compose (pakiet `docker-compose-plugin`, patrz https://docs.docker.com/compose/install/linux/).

Sprawdź, czy działa. Otwórz terminal (PowerShell, Terminal) i wpisz:

```sh
docker --version
docker compose version
```

Powinieneś zobaczyć dwie linie z numerami wersji, np. `Docker version 27.x.x` i `Docker Compose version v2.x.x`. Jeśli widzisz błąd „command not found", instalacja się nie udała albo trzeba otworzyć nowy terminal.

> Zamiast Dockera możesz użyć Podmana z `podman-compose`. Polecenia są takie same (`podman-compose up -d`). Przy zmianach w pliku użyj `podman-compose up -d --force-recreate`, żeby kontenery na pewno się odtworzyły.

## Krok 2: Utwórz folder i pobierz docker-compose.yml

Potrzebujesz jednego pliku. Utwórz dla niego osobny folder i wejdź do niego:

```sh
mkdir spotkeeper
cd spotkeeper
```

Pobierz plik, jedną z metod.

Linux, macOS, Windows 10/11 (w Windows w PowerShellu użyj `curl.exe`):

```sh
curl -O https://raw.githubusercontent.com/4RV3/spotkeeper/main/docker-compose.yml
```

Windows PowerShell:

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/4RV3/spotkeeper/main/docker-compose.yml -OutFile docker-compose.yml
```

Bez terminala: otwórz https://github.com/4RV3/spotkeeper/blob/main/docker-compose.yml, kliknij „Download raw file" (albo „Raw" i Zapisz jako) i zapisz plik w tym folderze pod nazwą `docker-compose.yml`.

Sprawdź, że plik jest na miejscu:

```sh
ls docker-compose.yml
```

(Windows PowerShell: `dir docker-compose.yml`). Powinna pojawić się jego nazwa.

## Krok 3: Uzupełnij domenę, login i hasło

Otwórz `docker-compose.yml` w edytorze tekstu (Notatnik, VS Code, `nano`). Zmieniasz trzy wartości. Zaraz na początku pliku jest domena:

```yaml
x-domain: &domain "twoja-domena.pl"   # <- wpisz domenę serwisu rezerwacji (bez https://), sugerowana to app.pa****sh.io
```

W sekcji `spotkeeper:` → `environment:` są login i hasło:

```yaml
      ACCOUNT_LOGIN: "twoj@email.pl"    # <- wpisz
      ACCOUNT_PASSWORD: "twoje-haslo"   # <- wpisz
```

Po zmianie wygląda to np. tak (wartości są przykładowe, wpisz swoje):

```yaml
x-domain: &domain "app.pa****sh.io"

      ACCOUNT_LOGIN: "jan.kowalski@example.com"
      ACCOUNT_PASSWORD: "MojeHaslo123"
```

Zasady:

- Domena to sama nazwa hosta: bez `https://`, bez `/` na końcu, bez ścieżki.
- Login musi być adresem e-mail.
- Wartości zostaw w cudzysłowie.
- Jeśli hasło zawiera znak `$`, zapisz go jako `$$` (inaczej Compose potraktuje go jako zmienną). Np. hasło `ab$cd` wpisz jako `"ab$$cd"`.
- Hasło z `"` w środku zapisz ze znakiem ucieczki `\"` albo zmień je w serwisie.
- **Opcjonalnie** zmień `APP_PASSWORD` (hasło do panelu SpotKeepera, domyślnie `spotkeeper`, minimum 8 znaków). Zrób to na pewno, jeśli panel będzie dostępny z internetu.

Jeśli zostawisz `twoja-domena.pl`, `twoj@email.pl` lub `twoje-haslo`, aplikacja **nie uruchomi się** i w logach (`docker compose logs spotkeeper`) zobaczysz komunikat, że w `docker-compose.yml` wciąż są wartości przykładowe. To samo przy pustej wartości, domenie z `https://` albo loginie, który nie jest adresem e-mail.

## Krok 4: Uruchom

W folderze z plikiem wpisz:

```sh
docker compose up -d
```

Przy pierwszym starcie Docker pobiera dwa obrazy (aplikacja jest mała, ale obraz z przeglądarką dla usługi `turnstile` jest duży). Może to potrwać kilka minut. Gdy skończy, sprawdź stan:

```sh
docker compose ps
```

Oczekiwany wynik: dwie usługi, `spotkeeper` i `turnstile`, obie w stanie `running` (`Up`), a przy `turnstile` dopisek `(healthy)`. Zaraz po starcie `turnstile` może mieć `(health: starting)`. Poczekaj minutę (usługa ma 60 s na rozruch) i sprawdź ponownie. Aplikacja `spotkeeper` startuje dopiero, gdy `turnstile` jest zdrowy.

Otwórz w przeglądarce **http://localhost:8080** (gdy SpotKeeper stoi na innym komputerze: `http://<adres-IP-serwera>:8080`). Powinieneś zobaczyć ekran „Zaloguj się".

Gdy coś nie działa, zobacz [Rozwiązywanie problemów](#rozwiązywanie-problemów).

## Krok 5: Zaloguj się do panelu

Wpisz hasło do panelu: wartość `APP_PASSWORD` z pliku. Jeśli go nie zmieniałeś, to `spotkeeper`. **Zmień je** (w `docker-compose.yml`, potem `docker compose up -d`), zwłaszcza gdy panel jest dostępny poza Twoją siecią. Po 5 błędnych próbach logowanie jest blokowane na 15 minut. Zalogowana sesja trwa 30 dni.

Po zalogowaniu zobaczysz stan, kalendarz, historię rezerwacji i kartę **Ustawienia**.

## Krok 6: Skonfiguruj w panelu

W karcie **Ustawienia** uzupełnij po kolei:

1. **Parking.** Wybierz parking z listy (lista jest pobierana z serwisu, widzisz tylko parkingi, do których masz dostęp). Jeśli lista się nie wczytuje, rozwiń **Zaawansowane** i wpisz identyfikator parkingu (GUID, 36 znaków) w polu „Identyfikator parkingu (ręcznie)". Wpisany ręcznie ma pierwszeństwo przed listą.
2. **Godzina otwarcia rezerwacji** (GG:MM, czas polski, domyślnie 11:00). To godzina, o której serwis otwiera rezerwację nowego dnia. Sprawdź ją w aplikacji serwisu: zobacz, o której pojawiają się miejsca na dzień odległy o 28 dni. Wpisz tę godzinę dokładnie, bo SpotKeeper nie pobiera jej z serwisu.
3. **Kwota płatności** (zł, domyślnie 15). Musi zgadzać się z ceną miejsca na Twoim parkingu. SpotKeeper nie pobiera ceny z serwisu, tylko płaci wpisaną kwotę zapisaną kartą. Dozwolone: od 0,01 do 1000 zł (separator może być kropką albo przecinkiem).
4. **Numer rejestracyjny.** Taki sam jak przy samochodzie w aplikacji serwisu (spacje i wielkość liter nie mają znaczenia).
5. **Poziom.** „dowolny" albo konkretny poziom parkingu. Lista poziomów jest pobierana z dnia, w którym jest najwięcej wolnych miejsc. Jeśli akurat nic nie jest wolne, lista bywa pusta. Zapisany wcześniej poziom zostaje wtedy zachowany. Poziom działa jak filtr tekstowy (wielkość liter nie ma znaczenia). Miejsca dla niepełnosprawnych, z ładowarką i mini-EV są zawsze pomijane. Spośród pasujących wolnych miejsc SpotKeeper losuje jedno.
6. **Dni rezerwacji.** Zaznacz dni tygodnia, **na które** chcesz mieć miejsce. Dzień, na który rezerwujesz, jest zawsze 28 dni po dniu uruchomienia (np. w poniedziałek rezerwacja otwiera dzień za 28 dni, czyli też poniedziałek). Gdy nic nie zaznaczysz, nic nie będzie rezerwowane.
7. **Aktywne.** Włącznik automatu. Po pierwszym uruchomieniu jest włączony.
8. **Pomijaj święta.** Gdy włączone, w polskie dni ustawowo wolne automat nie rezerwuje.
9. **Urlopy.** W kalendarzu kliknij „+ urlop" na dniu, który ma zostać pominięty (usuwasz kliknięciem „× urlop"). Przycisk jest tylko na dniach, które automat by zarezerwował i których rezerwacja jeszcze się nie otworzyła (do roku naprzód).
10. **Zaawansowane.** Ręczny identyfikator parkingu (patrz wyżej) i **Adres IP** płatności. Puste pole oznacza wykrycie automatyczne (usługa z `PUBLIC_IP_URL`, domyślnie api.ipify.org). Wpisuj tylko adres IPv4, jeśli go znasz.

Kliknij **Zapisz**. Na górze pojawi się „Zapisano ustawienia." Zmiany działają od razu, bez restartu. Przy błędzie (np. zły format godziny albo GUID) zobaczysz komunikat i nic się nie zapisze.

## Krok 7: Sprawdź konfigurację

Kliknij **Sprawdź konfigurację** (strona `/check`), potem **Sprawdź**. Kontrola niczego nie rezerwuje ani nie opłaca. Wykonuje po kolei sześć kroków i zatrzymuje się na pierwszym błędzie (kolejne są wtedy „Pominięto"):

| Krok | Co sprawdza | Co zrobić przy ✗ |
| --- | --- | --- |
| Ustawienia | czy jest numer rejestracyjny i parking; pokazuje też IP płatności | uzupełnij brakujące pole i kliknij Zapisz |
| Logowanie do serwisu | czy `ACCOUNT_LOGIN` i `ACCOUNT_PASSWORD` działają | popraw login, hasło lub `API_DOMAIN` w `docker-compose.yml`, potem `docker compose up -d` |
| Pojazd i karta | czy w serwisie jest samochód z tym numerem i aktywna karta | dodaj samochód lub kartę w aplikacji serwisu albo popraw numer w Ustawieniach |
| Parking | czy parking odpowiada | sprawdź parking i to, czy konto ma do niego dostęp |
| Poziom | czy wybrany poziom istnieje na parkingu | wybierz inny poziom albo „dowolny" (gdy nie ma wolnych miejsc, krok przechodzi z ostrzeżeniem) |
| Turnstile | czy usługa `turnstile` zwraca token | sprawdź `docker compose ps` i `docker compose logs turnstile` (patrz [Rozwiązywanie problemów](#rozwiązywanie-problemów)) |

Gdy wszystkie kroki mają ✓, pojawia się „Konfiguracja poprawna". Gotowe: SpotKeeper będzie rezerwował sam.

## Jak to działa na co dzień

1. Co dzień, ok. 10 minut przed godziną otwarcia (`PREPARE_LEAD`), SpotKeeper loguje się do serwisu i przygotowuje sesję.
2. Dniem docelowym jest dzień 28 dni później. Jeśli wypada w zaznaczonym dniu tygodnia, nie jest urlopem ani (przy włączonym „Pomijaj święta") świętem, program rezerwuje.
3. Dokładnie o godzinie otwarcia odpytuje serwis o wolne miejsca, wybiera jedno (z pominięciem miejsc dla niepełnosprawnych, z ładowarką i mini-EV; zgodne z wybranym poziomem), rozwiązuje zabezpieczenie Turnstile przez usługę `turnstile` i rezerwuje. Robi do 10 prób, w sumie do 7,5 minuty. Jeśli serwis dwa razy odpowie, że masz już rezerwację na ten czas, dzień dostaje status „pominięto".
4. Po udanej rezerwacji płaci wpisaną kwotą zapisaną kartą (do 5 prób startu płatności) i sprawdza status co 2 sekundy, do 20 razy.

W kalendarzu i w historii (tabela „Ostatnie rezerwacje", kolumny: dzień, typ, status, próby, miejsce, szczegóły) zobaczysz status:

| Status | Znaczenie |
| --- | --- |
| oczekuje | zaplanowane, jeszcze nie ruszyło |
| w toku | rezerwacja trwa |
| opłacono | miejsce zarezerwowane i opłacone |
| niepotwierdzona (płatność) | miejsce zarezerwowane, ale serwis nie potwierdził płatności albo nie udało się jej rozpocząć |
| nieudana | nie udało się zarezerwować; powód w kolumnie Szczegóły |
| anulowana | zadanie przerwano (np. zatrzymanie aplikacji) |
| pominięto | urlop, święto albo masz już rezerwację na ten dzień |

**Niepotwierdzona płatność.** SpotKeeper sprawdza ją jeszcze raz po 5 i po 30 minutach (także po restarcie aplikacji). Jeśli płatność przeszła, status zmienia się na „opłacono". Jeśli nie, w szczegółach pojawia się „płatność nadal niepotwierdzona po 30 min — sprawdź w aplikacji serwisu". Wtedy otwórz aplikację serwisu i sprawdź (opłać) rezerwację ręcznie.

Legendę znaczków w kalendarzu otworzysz przyciskiem **Legenda** pod kalendarzem.

## Rozwiązywanie problemów

Logi: `docker compose logs -f spotkeeper` oraz `docker compose logs -f turnstile` (wyjdź przez Ctrl+C). Poziom szczegółów zmieniasz przez `LOG_LEVEL`.

| Objaw | Przyczyna | Co zrobić |
| --- | --- | --- |
| `docker compose up -d` zgłasza błąd składni | złe wcięcie lub cudzysłów w `docker-compose.yml` | popraw plik; sprawdź go poleceniem `docker compose config -q` |
| `spotkeeper` ciągle się restartuje, w logach „config: ..." | wartości przykładowe lub błędna domena, login, hasło, `APP_PASSWORD` krótsze niż 8 znaków | popraw plik zgodnie z [Krokiem 3](#krok-3-uzupełnij-domenę-login-i-hasło), potem `docker compose up -d` |
| Strona http://localhost:8080 się nie otwiera | kontener nie działa albo port 8080 jest zajęty | `docker compose ps`; gdy port zajęty, zmień w pliku `"8080:8080"` na np. `"8081:8080"` |
| `turnstile` zostaje `unhealthy` lub `starting` | za mało pamięci, pierwszy rozruch, błąd przeglądarki | poczekaj 1-2 minuty; `docker compose logs turnstile`; zapewnij ok. 2 GB RAM; na arm64 patrz [turnstile/README.md](turnstile/README.md) |
| „Zaloguj się" odrzuca hasło | to hasło do panelu (`APP_PASSWORD`), nie do serwisu; po 5 błędach blokada na 15 min | wpisz wartość z pliku; poczekaj 15 minut |
| Po zmianie `APP_PASSWORD` panel wylogował | zmiana hasła unieważnia wszystkie sesje | zaloguj się nowym hasłem |
| Lista parkingów pusta lub „Nie udało się pobrać listy parkingów" | błędny login/hasło/domena albo serwis nie odpowiada | uruchom Sprawdź konfigurację; w razie potrzeby wpisz GUID ręcznie (Zaawansowane) |
| Lista poziomów pusta | brak wolnych miejsc w żadnym dniu | to normalne; zapisany poziom jest zachowany, lub wybierz „dowolny" |
| Kontrola: ✗ Logowanie | zły login/hasło lub domena | popraw `ACCOUNT_LOGIN`, `ACCOUNT_PASSWORD`, `API_DOMAIN`; `docker compose up -d` |
| Kontrola: ✗ Pojazd i karta | brak samochodu o tym numerze lub aktywnej karty | dodaj w aplikacji serwisu albo popraw numer w Ustawieniach |
| Kontrola: ✗ Parking | zły parking lub brak dostępu | wybierz parking z listy; poproś administratora o dostęp |
| Kontrola: ✗ Turnstile | `turnstile` nie działa lub zły `TURNSTILE_URL` | `docker compose ps`, `docker compose logs turnstile` |
| Nic się nie rezerwuje o godzinie otwarcia | „Aktywne" wyłączone, brak zaznaczonych dni, zła godzina, urlop/święto, komputer wyłączony | sprawdź kartę Stan („Następna rezerwacja automatyczna"), Ustawienia i `docker compose ps` |
| Status „nieudana", szczegóły „Reservation failed after 10 attempts" lub „budget exhausted" | nie było wolnych miejsc (pasujących do poziomu) w czasie prób | rozważ poziom „dowolny"; sprawdź godzinę otwarcia |
| Status „nieudana", „Context verification failed: ..." | usługa `turnstile` nie dostarczyła tokenu | `docker compose logs turnstile` |
| Status „niepotwierdzona" | serwis nie potwierdził płatności | poczekaj na kontrole po 5 i 30 min; potem sprawdź w aplikacji serwisu; upewnij się, że kwota zgadza się z ceną miejsca |
| Status „pominięto" | urlop, święto lub masz już rezerwację na ten dzień | nic, to nie błąd |

## Aktualizacja, kopia zapasowa, odinstalowanie

**Aktualizacja** (w folderze z `docker-compose.yml`):

```sh
docker compose pull && docker compose up -d
```

Ustawienia i historia zostają w wolumenie z danymi.

**Kopia zapasowa.** Baza (`spotkeeper.db`) i klucz sesji (`session.key`) leżą w wolumenie `spotkeeper-data` (w kontenerze `/data`). Znajdź pełną nazwę wolumenu (zwykle `<nazwa-folderu>_spotkeeper-data`):

```sh
docker volume ls
```

Zrób kopię do pliku w bieżącym folderze (podstaw nazwę wolumenu; najlepiej zatrzymaj wcześniej aplikację: `docker compose stop spotkeeper`):

```sh
docker run --rm -v spotkeeper_spotkeeper-data:/data:ro -v "$PWD":/backup alpine tar czf /backup/spotkeeper-backup.tgz -C /data .
```

(Windows PowerShell: zamiast `"$PWD"` użyj `${PWD}`.) Potem uruchom aplikację z powrotem: `docker compose start spotkeeper`.

**Zmiana hasła do panelu.** Zmień `APP_PASSWORD` w pliku i wykonaj `docker compose up -d`. Sekret sesji powstaje z `session.key` i `APP_PASSWORD`, więc **wszyscy zostaną wylogowani**. To samo spowoduje usunięcie `session.key`.

**Zmiana loginu, hasła lub domeny serwisu.** Edytuj plik i wykonaj `docker compose up -d`.

**Zatrzymanie** (dane zostają): `docker compose down`.

**Odinstalowanie razem z danymi** (usuwa ustawienia i historię!):

```sh
docker compose down -v
```

Potem możesz usunąć folder i obrazy (`docker image rm ghcr.io/4rv3/spotkeeper ghcr.io/4rv3/spotkeeper-turnstile`).

## Dostęp z internetu

Domyślnie panel jest dostępny po zwykłym HTTP na porcie 8080. **Nie wystawiaj go tak do internetu.** Jeśli musisz, to tylko za reverse proxy z HTTPS i z silnym `APP_PASSWORD`. Nie publikuj portu usługi `turnstile` (w compose go nie ma).

W `docker-compose.yml` odkomentuj i ustaw:

```yaml
      COOKIE_SECURE: "true"
      TRUSTED_PROXIES: "172.18.0.1"   # adres, z którego proxy łączy się z kontenerem
```

- `COOKIE_SECURE: "true"` oznacza ciasteczko sesji jako „tylko HTTPS". Ustaw je dopiero, gdy panel naprawdę otwierasz przez HTTPS, inaczej nie zalogujesz się.
- `TRUSTED_PROXIES` to adresy IP lub sieci CIDR (po przecinku), którym wolno ustawiać nagłówek `X-Forwarded-For`. Dzięki temu limit błędnych logowań działa na prawdziwy adres klienta. Adres proxy zależy od Twojej sieci (przy proxy na tym samym hoście zwykle jest to adres bramy sieci Dockera; sprawdzisz go poleceniem `docker network inspect`). Przykład powyżej jest tylko ilustracją.

Przykład z [Caddy](https://caddyserver.com/) na tym samym serwerze (HTTPS z certyfikatem Let's Encrypt automatycznie). Plik `Caddyfile`:

```
spotkeeper.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Aby port 8080 nie był widoczny z zewnątrz, w `docker-compose.yml` zmień `"8080:8080"` na `"127.0.0.1:8080:8080"`.

## Wszystkie zmienne środowiskowe

Ustawiasz je w sekcji `environment:` usługi `spotkeeper` w `docker-compose.yml` (zaawansowane są tam zakomentowane, z wartościami domyślnymi; odkomentuj, by zmienić). Po zmianie uruchom `docker compose up -d`. Czasy zapisujemy w formacie Go, np. `500ms`, `30s`, `10m`.

| Zmienna | Wymagana | Domyślnie | Opis |
| --- | --- | --- | --- |
| `API_DOMAIN` | tak | brak (w pliku `twoja-domena.pl` — trzeba zmienić) | domena serwisu rezerwacji, sama nazwa hosta (małe litery, bez `https://` i ścieżki); z niej wynikają adres API i dozwolony prefiks adresów turnstile. W compose ustawiana raz na górze pliku i przekazywana też do `turnstile` |
| `ACCOUNT_LOGIN` | tak | brak (w pliku `twoj@email.pl` — trzeba zmienić) | login konta w serwisie; musi być adresem e-mail (zapisywany małymi literami) |
| `ACCOUNT_PASSWORD` | tak | brak (w pliku `twoje-haslo` — trzeba zmienić) | hasło konta w serwisie |
| `APP_PASSWORD` | nie | `spotkeeper` | hasło do panelu, min. 8 znaków; przy domyślnym w logu jest ostrzeżenie `default_app_password` |
| `TZ` | nie | `Europe/Warsaw` (w compose) | strefa czasowa kontenera. Godziny w panelu są zawsze liczone w czasie polskim |
| `LISTEN_ADDR` | nie | `:8080` | adres nasłuchu aplikacji |
| `DB_PATH` | nie | `/data/spotkeeper.db` | ścieżka bazy SQLite |
| `COOKIE_SECURE` | nie | `false` | `true`, gdy panel stoi za HTTPS |
| `TRUSTED_PROXIES` | nie | puste | adresy IP lub CIDR zaufanych proxy (po przecinku), którym wolno ustawiać `X-Forwarded-For` |
| `LOG_LEVEL` | nie | `info` | `debug`, `info`, `warn` lub `error` |
| `API_BASE` | nie | `https://<API_DOMAIN>/api` | nadpisanie adresu API (używane przez demo i testy) |
| `PUBLIC_IP_URL` | nie | `https://api.ipify.org` | usługa zwracająca publiczny adres IP jako tekst; używana, gdy pole IP w Ustawieniach jest puste. Ustaw puste (`PUBLIC_IP_URL: ""`) albo `off`, by wyłączyć wykrywanie |
| `TURNSTILE_URL` | nie | `http://turnstile:3099` | adres usługi turnstile |
| `POLL_INTERVAL` | nie | `500ms` | jak często odpytywać o wolne miejsca |
| `PREPARE_LEAD` | nie | `10m` | ile przed godziną otwarcia przygotować sesję |
| `MAX_TOKEN_AGE` | nie | `30s` | maksymalny wiek tokenu turnstile |
| `APP_VERSION` | nie | `4.7.1` | wersja aplikacji wysyłana do API serwisu (zmieniaj tylko, gdy serwis wymaga nowszej) |

Usługa `turnstile` ma własne zmienne (m.in. `PORT`, `WORKERS`, `CONTEXT_REQUEST_TIMEOUT_S`); opisuje je [turnstile/README.md](turnstile/README.md). Zwykle nie musisz ich zmieniać.

Godzina otwarcia i kwota płatności nie są zmiennymi środowiskowymi: ustawiasz je w panelu.

## Dla programistów

Domyślnie `docker-compose.yml` pobiera gotowe obrazy z GHCR (`ghcr.io/4rv3/spotkeeper` i `ghcr.io/4rv3/spotkeeper-turnstile`). Po sklonowaniu repozytorium zbudujesz je lokalnie nakładką `docker-compose.build.yml` (obrazy powstają z katalogów `app/` i `turnstile/`):

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

Testy:

```sh
cd app && go test ./...                       # aplikacja (Go)
cd turnstile && python3 -m unittest discover -s tests   # usługa turnstile (potrzebuje flask i camoufox)
```

CI (GitHub Actions, `.github/workflows/ci.yml`) na każdym pushu i pull requeście sprawdza `gofmt`, `go vet`, `go test -race`, testy usługi turnstile oraz poprawność plików compose.

**Wydania.** Wypchnięcie tagu `v*` (np. `v1.2.3`) uruchamia workflow Release, który publikuje obrazy `spotkeeper` i `spotkeeper-turnstile` (amd64 i arm64) w GHCR z tagami wersji i `latest`.

**Uwaga o arm64.** Obraz `turnstile` opiera się na archiwach Camoufox dla arm64, ale nie był jeszcze zbudowany ani przetestowany na tej architekturze. Uruchom go najpierw na hoście arm64 (szczegóły w [turnstile/README.md](turnstile/README.md)).

## Licencja

MIT, patrz [LICENSE](LICENSE).

Logo i ikony: emoji 👹 (U+1F479) z [Noto Emoji](https://github.com/googlefonts/noto-emoji), licencja Apache 2.0, patrz [NOTICE](app/internal/web/static/NOTICE-noto-emoji.txt).
