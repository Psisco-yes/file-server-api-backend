# Go File Server

## Opis Projektu

W pełni funkcjonalny, REST-owy serwer plików zbudowany w Go, inspirowany systemami takimi jak Google Drive. Zapewnia bezpieczne zarządzanie plikami, udostępnianie oraz aktualizacje w czasie rzeczywistym.

## Kluczowe Funkcjonalności

- **Zarządzanie Plikami i Folderami:** Rozbudowane operacje na plikach i folderach (tworzenie, listowanie, zmiana nazwy, przenoszenie).
- **Bezpieczeństwo:** Autentykacja oparta na JWT z rotacją refresh tokenów, zarządzanie sesjami, obsługa HTTPS.
- **Udostępnianie:** Możliwość udostępniania plików i folderów innym użytkownikom z dziedziczeniem uprawnień (read/write).
- **Funkcje UX:** Kosz z opcją przywracania, ulubione, pobieranie wielu plików/folderów jako archiwum ZIP.
- **System Czasu Rzeczywistego:**
  - **Dziennik Zdarzeń:** Umożliwia wydajną synchronizację dla klientów działających w trybie offline.
  - **WebSockets:** Natychmiastowe, ukierunkowane powiadomienia o wszystkich zmianach w systemie.
- **Zarządzanie Zasobami:** Limity miejsca (quotas) na użytkownika.
- **Monitoring:** Endpointy `/health` i `/metrics` (Prometheus).
- **Dokumentacja API:** Automatycznie generowana i interaktywna dokumentacja Swagger UI.
- **Pełne Testy:** Pokrycie kodu testami integracyjnymi (API i baza danych) oraz zestaw testów E2E w Postman.

## Stack Technologiczny

- **Backend:** Go (Golang)
- **Baza Danych:** PostgreSQL
- **Reverse Proxy (HTTPS):** Caddy
- **Konteneryzacja:** Docker & Docker Compose
- **Testowanie:** `testcontainers-go`, `testify`
- **Dokumentacja:** `swaggo`

## Uruchomienie

1.  Sklonuj repozytorium.
2.  Przejdź do folderu projektu: `cd go-file-server`
3.  Stwórz plik `.env` na podstawie `env.example` i uzupełnij wymagane sekrety (`POSTGRES_PASSWORD`, `JWT_SECRET`).
4.  (Opcjonalnie, dla HTTPS lokalnie) Wygeneruj lokalne certyfikaty za pomocą `mkcert` w folderze `certs`. Upewnij się, że nazwy plików pasują do tych w `docker-compose.yml`.
5.  Uruchom projekt:
    ```bash
    docker-compose up --build
    ```
- Serwer będzie dostępny pod adresem `https://localhost`.
- Dokumentacja API Swaggera jest dostępna pod adresem `https://localhost/swagger/index.html`.

Po pierwszym uruchomieniu, w systemie dostępne są domyślne konta do testowania (zgodnie z `db/init.sql`):
- **Użytkownik:** `admin`, **Hasło:** `admin`
- **Użytkownik:** `user`, **Hasło:** `user`

## Zarządzanie Administracyjne (Skrypty PowerShell)

Zarządzanie użytkownikami i systemem odbywa się za pomocą gotowych skryptów PowerShell (`*.ps1`), które znajdują się w folderze `/scripts`.

### Wymagania

*   Uruchomione kontenery (`docker-compose up`).
*   Terminal PowerShell.
*   Zmienne środowiskowe w pliku `.env` muszą być poprawnie ustawione.

---

### 1. Dodawanie Nowego Użytkownika

```powershell
.\scripts\add-user.ps1 -Username "nowyuser" -Password "SuperT@jneHaslo1" -DisplayName "Nowy Użytkownik"
```

### 2. Trwałe Usuwanie Użytkownika

**UWAGA: Ta operacja jest nieodwracalna!** Usuwa użytkownika, wszystkie jego pliki, udostępnienia i sesje.

```powershell
.\scripts\delete-user.ps1 -Username "nowyuser"
```

### 3. Zmiana Limitu Miejsca

Ustawia limit miejsca dla użytkownika w Gigabajtach (GB).

```powershell
.\scripts\change-quota.ps1 -Username "nowyuser" -QuotaGB 25
```

### 4. Resetowanie Hasła Użytkownika

```powershell
.\scripts\reset-password.ps1 -Username "nowyuser" -NewPassword "NoweLepszeHaslo_456"
```

### 5. Listowanie Wszystkich Użytkowników

```powershell
.\scripts\list-users.ps1
```

### 6. Wymuszone Wylogowanie Użytkownika

Natychmiast kończy wszystkie aktywne sesje dla danego użytkownika.

```powershell
.\scripts\terminate-sessions.ps1 -Username "nowyuser"
```

### 7. Statystyki Systemu

Wyświetla ogólne statystyki serwera.

```powershell
.\scripts\system-stats.ps1
```

## Przegląd API Endpoints

Wszystkie chronione endpointy wymagają nagłówka `Authorization: Bearer <access_token>`.

### Autentykacja i Sesje (`/auth`, `/sessions`)
- `POST /auth/login`: Logowanie.
- `POST /auth/refresh`: Odświeżanie tokena.
- `GET /sessions`: Listowanie aktywnych sesji.
- `POST /sessions/terminate_all`: Wyloguj wszędzie.
- `DELETE /sessions/{sessionId}`: Wyloguj konkretną sesję.

### Zarządzanie Użytkownikiem (`/me`)
- `GET /me`: Pobierz informacje o sobie.
- `GET /me/storage`: Sprawdź wykorzystanie miejsca.
- `PATCH /me/password`: Zmień hasło.

### Pliki i Foldery (`/nodes`)
- `GET /nodes`: Listuj własne pliki/foldery (z paginacją).
- `POST /nodes/folder`: Stwórz folder.
- `POST /nodes/file`: Wgraj plik(i).
- `GET /nodes/archive`: Pobierz archiwum ZIP.
- `GET /nodes/{id}/download`: Pobierz plik.
- `PATCH /nodes/{id}`: Zmień nazwę lub przenieś.
- `DELETE /nodes/{id}`: Przenieś do kosza.
- `POST /nodes/{id}/restore`: Przywróć z kosza.

### Udostępnianie (`/shares`)
- `POST /nodes/{id}/share`: Udostępnij plik/folder.
- `GET /shares/incoming/users`: Listuj, kto mi udostępnił.
- `GET /shares/incoming/nodes`: Przeglądaj, co mi udostępniono.
- `GET /shares/outgoing`: Listuj, co ja udostępniłem.
- `DELETE /shares/{id}`: Cofnij udostępnienie.

### Inne
- `GET /favorites`: Listuj ulubione.
- `POST /nodes/{id}/favorite`: Dodaj do ulubionych.
- `DELETE /nodes/{id}/favorite`: Usuń z ulubionych.
- `GET /trash`: Listuj zawartość kosza.
- `DELETE /trash/purge`: Opróżnij kosz.
- `GET /events`: Pobierz nowe zdarzenia do synchronizacji.
- `GET /ws`: Połączenie WebSocket.

---

## Aktualizacje w Czasie Rzeczywistym (WebSockets)

Serwer wykorzystuje WebSockets do natychmiastowego powiadamiania podłączonych klientów o wszystkich istotnych zdarzeniach w systemie. Eliminuje to potrzebę cyklicznego odpytywania endpointu `/events` (polling) i zapewnia płynne działanie interfejsu użytkownika.

### Nawiązywanie Połączenia

- **Endpoint:** `GET /ws` (protokół `wss://`)
- **URL Połączenia:** `wss://localhost/ws?token=<access_token>`

Uwierzytelnienie odbywa się poprzez przekazanie ważnego tokena dostępowego (JWT) jako parametru zapytania o nazwie `token`. Jeśli token jest nieprawidłowy lub wygasł, połączenie zostanie odrzucone.

### Format Komunikatów

Po nawiązaniu połączenia, komunikacja jest jednostronna – serwer wysyła komunikaty do klienta. Klient nie musi wysyłać żadnych wiadomości, jego jedynym zadaniem jest nasłuchiwanie.

Wszystkie komunikaty są wysyłane w formacie JSON i mają następującą strukturę:

```json
{
  "event_type": "nazwa_zdarzenia",
  "payload": { "some": "data" }
}
```

- `event_type` (string): Identyfikator typu zdarzenia (np. `node_created`, `node_trashed`).
- `payload` (object): Obiekt zawierający dane związane ze zdarzeniem. Jego struktura zależy od `event_type`.

### Przykładowe Zdarzenia

**1. Utworzono nowy plik/folder (`node_created`):**
```json
{
  "event_type": "node_created",
  "payload": {
    "id": "_vx2a-43VqRT5wz_s9u4",
    "owner_id": 1,
    "parent_id": "fLW5kAh2ia9vYmjMnU4nZ",
    "name": "Nowy Raport.docx",
    "node_type": "file",
    "size_bytes": 12345,
    "mime_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    "created_at": "2024-08-27T10:00:00Z",
    "modified_at": "2024-08-27T10:00:00Z"
  }
}
```

**2. Plik został przeniesiony do kosza (`node_trashed`):**
```json
{
  "event_type": "node_trashed",
  "payload": {
    "id": "_vx2a-43VqRT5wz_s9u4",
    "parent_id": "fLW5kAh2ia9vYmjMnU4nZ"
  }
}
```

**3. Ktoś cofnął Ci udostępnienie pliku (`share_revoked_for_you`):**
```json
{
  "event_type": "share_revoked_for_you",
  "payload": {
    "node_id": "zInneIDPliku987654321"
  }
}
```

---

## Roadmap / TODO

Lista zidentyfikowanych problemów i planowanych do wdrożenia funkcjonalności.

### Błędy Krytyczne i Ograniczenia do Naprawy

-   [ ] **Niekompletne przywracanie z kosza:** Przywrócenie usuniętego folderu odtwarza tylko sam folder, bez jego zawartości, co prowadzi do utraty danych. Należy zaimplementować rekurencyjne przywracanie.
-   [ ] **Błąd archiwizacji (ZIP) dla dużych folderów:** Funkcja pobierania archiwum ZIP jest ograniczona do 1000 elementów na folder, co skutkuje tworzeniem niekompletnych archiwów bez informowania o tym użytkownika.
-   [ ] **Brak obsługi dużych plików:** Obecne ograniczenie uploadu do 1 GB i brak mechanizmu "chunked upload" uniemożliwia wgrywanie większych plików i obciąża pamięć RAM serwera.
-   [ ] **Wysokie zużycie RAM przy archiwizacji:** Mechanizm tworzenia archiwum ZIP zbiera metadane wszystkich plików w pamięci przed rozpoczęciem pakowania, co może prowadzić do problemów z wydajnością przy bardzo dużej liczbie plików.
-   [ ] **Nieskuteczne unieważnianie sesji dla WebSockets:** Aktywne połączenia WebSocket nie są zamykane, gdy sesja użytkownika wygaśnie lub zostanie zdalnie zakończona (np. przez "wyloguj wszędzie"). Stwarza to lukę bezpieczeństwa, pozwalając na dalsze nasłuchiwanie zdarzeń pomimo unieważnienia sesji.

### Nowe Funkcje do Implementacji

-   [ ] **Kopiowanie Plików i Folderów:** Dodanie możliwości tworzenia kopii plików i całych struktur folderów.
-   [ ] **Wyszukiwarka Plików:** Zaimplementowanie endpointu pozwalającego na wyszukiwanie plików i folderów po nazwie w całej dostępnej przestrzeni użytkownika (własne i udostępnione).
-   [ ] **Dziennik Audytowy (Audit Log):** Stworzenie oddzielnego, niezmiennego dziennika zdarzeń związanych z bezpieczeństwem (logowanie, dostęp do plików, zmiany uprawnień) w celu zapewnienia rozliczalności i zgodności z RODO.
