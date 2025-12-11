# Go File Server

## Opis Projektu

W pełni funkcjonalny, REST-owy serwer plików zbudowany w Go, inspirowany systemami takimi jak Google Drive. Zapewnia bezpieczne zarządzanie plikami, udostępnianie oraz aktualizacje w czasie rzeczywistym.

## Kluczowe Funkcjonalności

- **Zarządzanie Plikami i Folderami:** Rozbudowane operacje na plikach i folderach (tworzenie, kopiowanie, listowanie, zmiana nazwy, przenoszenie).
- **Bezpieczeństwo:** Autentykacja oparta na JWT z rotacją refresh tokenów, zarządzanie sesjami, obsługa HTTPS.
- **Udostępnianie:** Możliwość udostępniania plików i folderów innym użytkownikom z dziedziczeniem uprawnień (`read`/`write`).
- **Funkcje UX:** Kosz z opcją przywracania, ulubione, wyszukiwarka, pobieranie wielu plików/folderów jako archiwum ZIP.
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

## Uruchomienie Serwera (Krok po Kroku)

Ten przewodnik zakłada, że serwer jest uruchamiany lokalnie.

### Wymagania Wstępne

1.  **Git** (do sklonowania repozytorium)
2.  **Docker** i **Docker Compose** (do uruchomienia kontenerów)
3.  **mkcert** (do wygenerowania lokalnie zaufanych certyfikatów SSL)

### Kroki Instalacyjne

1.  **Sklonuj Repozytorium:**
    ```bash
    git clone https://github.com/Psisco-yes/file-server-api-backend.git
    cd file-server-api-backend
    ```

2.  **Skonfiguruj Środowisko:**
    Skopiuj plik `env.example` i zmień jego nazwę na `.env`. Następnie otwórz plik `.env` i uzupełnij wymagane wartości:
    - `POSTGRES_PASSWORD`: Bezpieczne hasło dla bazy danych.
    - `JWT_SECRET`: Długi, losowy ciąg znaków do podpisywania tokenów JWT.

3.  **Wygeneruj Certyfikaty SSL:**
    *   Najpierw, jeśli robisz to po raz pierwszy, zainstaluj lokalny urząd certyfikacji `mkcert` (wymaga to uprawnień administratora):
        ```bash
        mkcert -install
        ```
    *   Następnie, w głównym folderze projektu, utwórz folder `certs` i wygeneruj pliki certyfikatu i klucza:
        ```bash
        mkdir certs
        mkcert -cert-file ./certs/cert.pem -key-file ./certs/key.pem localhost 127.0.0.1 ::1
        ```

4.  **Uruchom Aplikację:**
    Uruchom wszystkie usługi za pomocą Docker Compose. Proces ten automatycznie pobierze zależności, zbuduje obrazy i uruchomi kontenery.
    ```bash
    docker-compose up --build
    ```

Po pomyślnym uruchomieniu:
- Serwer będzie dostępny pod adresem `https://localhost`.
- Dokumentacja API Swaggera jest dostępna pod adresem `https://localhost/swagger/index.html`.

### Domyślne Konta
Po pierwszym uruchomieniu, w systemie dostępne są domyślne konta do testowania (zgodnie z `db/init.sql`):
- **Użytkownik:** `admin`, **Hasło:** `admin`
- **Użytkownik:** `user`, **Hasło:** `user`

---

## Wytyczne dla Klienta API

Aby zapewnić wydajne i responsywne działanie, aplikacja kliencka powinna stosować się do poniższych zasad.

### Architektura: Lokalny Cache i WebSockets

Aplikacja kliencka **musi** działać w oparciu o **lokalny cache** struktury plików, który jest synchronizowany w czasie rzeczywistym.

1.  **Start Aplikacji (Pierwsza Synchronizacja):**
    *   Pobierz całą strukturę plików i folderów użytkownika, rekurencyjnie wywołując `GET /api/v1/nodes` dla własnych zasobów oraz `GET /api/v1/shares/incoming/...` dla udostępnionych.
    *   Zbuduj w pamięci (lub lokalnej bazie) pełną kopię (cache) drzewa plików.
    *   Wywołaj `GET /api/v1/events/latest`, aby pobrać ID ostatniego zdarzenia. Zapisz tę wartość jako `last_known_event_id`.
    *   Nawiąż połączenie **WebSocket** (`wss://.../api/v1/ws?token=<token>`).

2.  **Działanie Aplikacji:**
    *   Wszystkie operacje w UI (wyświetlanie folderów, etc.) wykonuj na **lokalnym cache'u**.
    *   Gdy serwer prześle wiadomość przez WebSocket, zaktualizuj swój lokalny cache na podstawie `event_type` i `payload`.
    *   Gdy użytkownik wykonuje akcję (np. tworzy folder), wyślij odpowiedni request do API. **Nie modyfikuj cache'u od razu.** Poczekaj na wiadomość zwrotną z WebSocket, która będzie ostatecznym potwierdzeniem, że operacja na serwerze się powiodła.

3.  **Synchronizacja po Powrocie Online:**
    *   Nawiąż połączenie WebSocket.
    *   Wywołuj w pętli `GET /api/v1/events?since=<last_known_event_id>`, pobierając zdarzenia w paczkach, aż serwer zwróci pustą listę. Zaktualizuj `last_known_event_id` po każdej paczce.

### Zarządzanie Tokenami

-   **Access Token (15 minut):** Używaj go do wszystkich zapytań API. Odświeżaj go **proaktywnie** (np. co 10-14 minut), nie czekając na błąd `401`.
-   **Refresh Token (24 godziny):** Służy tylko do odświeżania. Po każdym użyciu endpointu `/auth/refresh` otrzymasz **nowy** refresh token – stary staje się nieważny. Musisz zapisać ten nowy.
-   **Wylogowanie:** Po wywołaniu `DELETE /sessions/...` lub `POST /sessions/terminate_all`, klient **musi** usunąć oba tokeny ze swojej pamięci.

---


## Zarządzanie Administracyjne (Skrypty PowerShell)

Zarządzanie użytkownikami i systemem odbywa się za pomocą gotowych skryptów PowerShell (`*.ps1`), które znajdują się w folderze `/scripts`.

### Wymagania

*   Uruchomione kontenery (`docker-compose up`).
*   Terminal PowerShell.
*   Zmienne środowiskowe w pliku `.env` muszą być poprawnie ustawione.

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

---

## Przegląd API Endpoints

Wszystkie ścieżki są poprzedzone `/api/v1`. Wszystkie chronione endpointy wymagają nagłówka `Authorization: Bearer <access_token>`.

### Autentykacja i Sesje
- `POST /auth/login`: Logowanie.
- `POST /auth/refresh`: Odświeżanie tokena.
- `GET /sessions`: Listowanie aktywnych sesji.
- `POST /sessions/terminate_all`: Wyloguj wszędzie.
- `DELETE /sessions/{sessionId}`: Wyloguj konkretną sesję.

### Zarządzanie Profilem Użytkownika
- `GET /me`: Pobierz aktualne informacje o sobie.
- `PATCH /me`: Zaktualizuj profil (np. `display_name`).
- `GET /me/storage`: Sprawdź wykorzystanie miejsca.
- `PATCH /me/password`: Zmień hasło.

### Pliki i Foldery
- `GET /nodes`: Listuj własne pliki/foldery (z paginacją).
- `POST /nodes/folder`: Stwórz folder.
- `POST /nodes/file`: Wgraj plik(i).
- `GET /nodes/archive`: Pobierz archiwum ZIP.
- `GET /nodes/{nodeId}`: Pobierz metadane obiektu.
- `GET /nodes/{nodeId}/path`: Pobierz ścieżkę "breadcrumbs".
- `GET /nodes/{nodeId}/download`: Pobierz plik.
- `PATCH /nodes/{nodeId}`: Zmień nazwę lub przenieś.
- `DELETE /nodes/{nodeId}`: Przenieś do kosza.
- `POST /nodes/{nodeId}/restore`: Przywróć z kosza.
- `POST /nodes/{nodeId}/copy`: Stwórz głęboką kopię.

### Udostępnianie
- `POST /nodes/{nodeId}/share`: Udostępnij plik/folder.
- `GET /nodes/{nodeId}/shares`: Listuj udostępnienia dla danego obiektu.
- `GET /shares/incoming/users`: Listuj, kto mi udostępnił.
- `GET /shares/incoming/nodes`: Przeglądaj, co mi udostępniono.
- `GET /shares/outgoing`: Listuj, co ja udostępniłem.
- `DELETE /shares/{shareId}`: Cofnij udostępnienie.

### Funkcje Dodatkowe
- `GET /search`: Wyszukaj pliki i foldery.
- `GET /favorites`: Listuj ulubione.
- `POST /nodes/{nodeId}/favorite`: Dodaj do ulubionych.
- `DELETE /nodes/{nodeId}/favorite`: Usuń z ulubionych.
- `GET /trash`: Listuj zawartość kosza.
- `DELETE /trash/purge`: Opróżnij kosz.

### Systemowe
- `GET /events`: Pobierz nowe zdarzenia do synchronizacji.
- `GET /events/latest`: Pobierz ID ostatniego zdarzenia.
- `GET /ws`: Połączenie WebSocket.

---

## Aktualizacje w Czasie Rzeczywistym (WebSockets)

Serwer wykorzystuje WebSockets do natychmiastowego powiadamiania podłączonych klientów o wszystkich istotnych zdarzeniach w systemie.

### Nawiązywanie Połączenia

- **Endpoint:** `GET /api/v1/ws` (protokół `wss://` dla HTTPS)
- **URL Połączenia:** `wss://localhost/api/v1/ws?token=<access_token>`

Uwierzytelnienie odbywa się poprzez przekazanie ważnego tokena dostępowego (JWT) jako parametru zapytania o nazwie `token`. Jeśli token jest nieprawidłowy, wygasł lub sesja została unieważniona, połączenie zostanie odrzucone lub zamknięte.

### Format Komunikatów

Po nawiązaniu połączenia, komunikacja jest jednostronna – serwer wysyła komunikaty do klienta. Klient nie musi wysyłać żadnych wiadomości, jego jedynym zadaniem jest nasłuchiwanie. Wszystkie komunikaty są wysyłane w formacie JSON i mają następującą strukturę:

```json
{
  "event_type": "nazwa_zdarzenia",
  "payload": { "dane_zwiazane_ze_zdarzeniem" }
}
```

### Katalog Zdarzeń i Struktura Payloadów

Poniżej znajduje się kompletna lista wszystkich typów zdarzeń (`event_type`) i opis ich `payload`.

| Event Type | Opis | Struktura `payload` | Odbiorcy |
| :--- | :--- | :--- | :--- |
| **`node_created`** | Utworzono nowy plik lub folder. | Pełny obiekt `Node`. | Twórca, Właściciel folderu nadrzędnego |
| **`nodes_copied`** | Skopiowano jeden lub więcej plików/folderów. | Tablica `[]` pełnych obiektów `Node`. | Kopiujący, Właściciel folderu docelowego |
| **`node_renamed`** | Zmieniono nazwę pliku/folderu. | `{ "id", "new_name", "old_name" }` | Osoba modyfikująca, Właściciel |
| **`node_moved`** | Przeniesiono plik/folder. | `{ "id", "new_parent_id", "old_parent_id" }` | Osoba modyfikująca, Właściciel |
| **`node_trashed`** | Przeniesiono plik/folder do kosza. | `{ "id", "parent_id" }` | Osoba usuwająca, Właściciel |
| **`node_restored`** | Przywrócono plik/folder z kosza. | Pełny obiekt `Node`. | Właściciel |
| **`favorite_added`** | Dodano obiekt do ulubionych. | `{ "node_id" }` | Tylko osoba wykonująca akcję |
| **`favorite_removed`** | Usunięto obiekt z ulubionych. | `{ "node_id" }` | Tylko osoba wykonująca akcję |
| **`node_shared_with_you`** | Ktoś udostępnił Ci zasób. | `{ "share_info", "node_info" }` | Tylko Odbiorca udostępnienia |
| **`node_share_created`** | Potwierdzenie, że udostępniłeś zasób. | `{ "share_info", "node_info", "recipient_username" }` | Tylko Udostępniający |
| **`share_revoked_for_you`** | Ktoś cofnął dla Ciebie udostępnienie. | `{ "node_id" }` | Tylko Odbiorca udostępnienia |
| **`node_share_revoked`** | Potwierdzenie, że cofnąłeś udostępnienie. | `{ "share_id", "node_id" }` | Tylko Udostępniający |

---

## Roadmap / TODO

Lista zidentyfikowanych ograniczeń i planowanych do wdrożenia funkcjonalności, które wykraczają poza obecny zakres projektu.

### Ograniczenia do Naprawy w Przyszłości

-   [ ] **Niekompletne przywracanie z kosza:** Przywrócenie usuniętego folderu odtwarza tylko sam folder, bez jego zawartości. W przyszłości należy zaimplementować rekurencyjne przywracanie z obsługą konfliktów nazw.
-   [ ] **Brak obsługi bardzo dużych plików:** Obecne ograniczenie uploadu (aktualnie 1 GB na cały request) i brak mechanizmu "chunked upload" uniemożliwia wgrywanie plików o dużym rozmiarze.
-   [ ] **Wysokie zużycie RAM przy archiwizacji:** Mechanizm tworzenia archiwum ZIP może być nieefektywny przy bardzo dużych strukturach folderów.
-   [ ] **Natychmiastowe unieważnianie tokenów (Blacklisting):** Obecnie `access token` jest ważny do momentu naturalnego wygaśnięcia. W przyszłości można zaimplementować mechanizm "czarnej listy" do natychmiastowego unieważniania tokenów.

### Nowe Funkcje do Implementacji w Przyszłości

-   [ ] **Filtrowanie i Sortowanie Wyników:** Rozbudowa istniejących endpointów listujących o zaawansowane opcje filtrowania i sortowania.
-   [ ] **Dziennik Audytowy (Audit Log):** Stworzenie oddzielnego, niezmiennego dziennika zdarzeń krytycznych dla bezpieczeństwa.