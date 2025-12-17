# Go File Server

## Opis Projektu

W pełni funkcjonalny, REST-owy serwer plików zbudowany w Go, inspirowany systemami takimi jak Google Drive. Zapewnia bezpieczne zarządzanie plikami, udostępnianie oraz aktualizacje w czasie rzeczywistym, zoptymalizowane pod kątem wydajności i łatwej integracji z aplikacjami klienckimi.

## Kluczowe Funkcjonalności

-   **Zaawansowane Zarządzanie Plikami:** Pełen zestaw operacji CRUD na plikach i folderach.
-   **Obsługa Dużych Plików:** Wsparcie dla przesyłania bardzo dużych plików dzięki mechanizmowi "chunked uploads", z możliwością wznawiania.
-   **Sortowanie po Stronie Serwera:** Wszystkie endpointy listujące obsługują zaawansowane sortowanie wielokolumnowe (np. `?sort=type,-name`).
-   **Bezpieczeństwo:**
    *   **Uwierzytelnianie JWT:** Zabezpieczenie oparte na tokenach z rotacją i krótkim czasem życia `access token`.
    *   **Zarządzanie Sesjami:** Możliwość przeglądania i unieważniania aktywnych sesji.
    *   **HTTPS:** Domyślna obsługa szyfrowanego połączenia dzięki integracji z Caddy.
    *   **Rate Limiting:** Wbudowana ochrona przed atakami typu brute-force i DoS.
-   **Elastyczne Udostępnianie:** Możliwość udostępniania plików i folderów z uprawnieniami (`read`/`write`) i dziedziczeniem.
-   **Użyteczne Funkcje:**
    *   **Kosz:** Funkcjonalność "miękkiego usuwania" z opcją rekurencyjnego przywracania.
    *   **Ulubione:** Oznaczanie ważnych plików i folderów.
    *   **Wyszukiwarka:** Globalne wyszukiwanie po nazwie we własnych i udostępnionych zasobach.
    *   **Archiwizator ZIP:** Pobieranie wielu plików i folderów jako pojedynczego archiwum `.zip`.
-   **System Czasu Rzeczywistego:**
    *   **Dziennik Zdarzeń:** Umożliwia wydajną synchronizację "catch-up" dla klientów offline.
    *   **WebSockets:** Natychmiastowe, ukierunkowane powiadomienia o wszystkich zmianach w systemie.
-   **Zarządzanie Zasobami:** Limity miejsca (quotas) na użytkownika.
-   **Monitoring i Diagnostyka:** Endpointy `/health` i `/metrics` (w formacie Prometheus).
-   **Dokumentacja API:** Automatycznie generowana i interaktywna dokumentacja Swagger UI.
-   **Pełne Pokrycie Testami:** Wysokie pokrycie kodu testami integracyjnymi i jednostkowymi.

## Stack Technologiczny

-   **Backend:** Go (Golang) 1.25.5
-   **Baza Danych:** PostgreSQL 17
-   **Reverse Proxy (HTTPS):** Caddy 2.10.2
-   **Konteneryzacja:** Docker & Docker Compose
-   **Testowanie:** `testcontainers-go`, `testify`
-   **Dokumentacja:** `swaggo`

## Uruchomienie Serwera (Krok po Kroku)

Ten przewodnik zakłada, że serwer jest uruchamiany lokalnie.

### Wymagania Wstępne

1.  **Git**
2.  **Docker** i **Docker Compose**
3.  **mkcert**

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
    *   Zainstaluj lokalny urząd certyfikacji `mkcert`:
        ```bash
        mkcert -install
        ```
    *   W głównym folderze projektu, utwórz folder `certs` i wygeneruj pliki:
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
Dostępne są domyślne konta do testowania (zgodnie z `db/init.sql`):
- **Użytkownik:** `admin`, **Hasło:** `admin`
- **Użytkownik:** `user`, **Hasło:** `user`

---

## Wytyczne dla Klienta API

Aby zapewnić wydajne i responsywne działanie, aplikacja kliencka powinna stosować się do poniższych zasad.

### Architektura: "Inteligentny Klient" z Odświeżaniem Danych

Zaleca się podejście "Inteligentnego Klienta" (Smart Client), które opiera się na cachowaniu poszczególnych zapytań API i inteligentnym ich odświeżaniu w odpowiedzi na zdarzenia z serwera.

1.  **Start Aplikacji:**
    *   Pobierz podstawowe dane, np. `GET /api/v1/me`, aby uzyskać informacje o zalogowanym użytkowniku.
    *   Nawiąż połączenie **WebSocket** (`wss://.../api/v1/ws?token=<token>`), aby nasłuchiwać na zmiany w czasie rzeczywistym.

2.  **Działanie Aplikacji:**
    *   Każdy widok (np. zawartość folderu) pobiera swoje dane z odpowiedniego endpointu (np. `GET /api/v1/nodes?parent_id=...`). Odpowiedź z tego zapytania jest cachowana po stronie klienta.
    *   Gdy serwer prześle wiadomość przez WebSocket (np. `node_created`), klient identyfikuje, którego widoku dotyczyła zmiana i **unieważnia cache** dla tego konkretnego zapytania, co powoduje automatyczne pobranie świeżych danych w tle.
    *   To podejście drastycznie upraszcza kod frontendu i czyni backend jedynym "źródłem prawdy" (Source of Truth).

3.  **Synchronizacja po Powrocie Online:**
    *   Po odzyskaniu połączenia, klient powinien odświeżyć wszystkie aktywne, widoczne dla użytkownika dane.
    *   Dodatkowo, można użyć `GET /api/v1/events?since=<last_known_event_id>`, aby zsynchronizować zmiany, które nastąpiły w tle.

### Zarządzanie Tokenami

-   **Access Token (15 minut):** Używaj go do wszystkich zapytań API. Zaleca się jego proaktywne odświeżanie.
-   **Refresh Token (24 godziny):** Służy wyłącznie do uzyskiwania nowego `access token`. Po każdym użyciu endpointu `/auth/refresh` otrzymasz **nowy** `refresh token`, który należy zapisać.
-   **Wylogowanie:** Po wywołaniu `DELETE /sessions/...` lub `POST /sessions/terminate_all`, klient **musi** usunąć oba tokeny ze swojej pamięci.

### Przesyłanie Plików (Chunked Upload)

Aby wgrać duży plik, użyj następującego przepływu:

1.  **Inicjacja:** Wyślij `POST /api/v1/nodes/upload/initiate` z metadanymi pliku (`name`, `size`, `parent_id`). W odpowiedzi otrzymasz `upload_id`.
2.  **Przesyłanie:** Podziel plik na części ("chunki"). Wysyłaj każdą część za pomocą `PATCH /api/v1/nodes/upload/{uploadId}`, dodając nagłówek `Content-Range` (np. `bytes 0-5242879/20000000`).
3.  **Finalizacja:** Po wgraniu ostatniej części, wyślij `POST /api/v1/nodes/upload/{uploadId}/complete`, aby zakończyć proces. W odpowiedzi otrzymasz pełny obiekt `RichNode` nowo utworzonego pliku.

Dla małych plików (np. < 100MB), nadal można używać prostszego endpointu `POST /api/v1/nodes/file`.

---

## Zarządzanie Administracyjne (Skrypty PowerShell)

Zarządzanie użytkownikami i systemem odbywa się za pomocą gotowych skryptów PowerShell (`*.ps1`), które znajdują się w folderze `/scripts`.

### Wymagania

*   Uruchomione kontenery (`docker-compose up`).
*   Terminal PowerShell.
*   Zmienne środowiskowe w pliku `.env` muszą być poprawnie ustawione.

### 1. Dodawanie Nowego Użytkownika

```powershell
.\add-user.ps1 -Username "nowyuser" -Password "SuperT@jneHaslo1" -DisplayName "Nowy Użytkownik"
```

### 2. Trwałe Usuwanie Użytkownika

**UWAGA: Ta operacja jest nieodwracalna!** Usuwa użytkownika, wszystkie jego pliki, udostępnienia i sesje.

```powershell
.\delete-user.ps1 -Username "nowyuser"
```

### 3. Zmiana Limitu Miejsca

Ustawia limit miejsca dla użytkownika w Gigabajtach (GB).

```powershell
.\change-quota.ps1 -Username "nowyuser" -QuotaGB 25
```

### 4. Resetowanie Hasła Użytkownika

```powershell
.\reset-password.ps1 -Username "nowyuser" -NewPassword "NoweLepszeHaslo_456"
```

### 5. Listowanie Wszystkich Użytkowników

```powershell
.\list-users.ps1
```

### 6. Wymuszone Wylogowanie Użytkownika

Natychmiast kończy wszystkie aktywne sesje dla danego użytkownika.

```powershell
.\terminate-sessions.ps1 -Username "nowyuser"
```

### 7. Statystyki Systemu

Wyświetla ogólne statystyki serwera.

```powershell
.\system-stats.ps1
```

### Przykłady Użycia

-   **Dodaj użytkownika:** `.\scripts\add-user.ps1 -Username "nowyuser" -Password "SuperT@jneHaslo1"`
-   **Usuń użytkownika:** `.\scripts\delete-user.ps1 -Username "nowyuser"`
-   **Zmień limit miejsca:** `.\scripts\change-quota.ps1 -Username "nowyuser" -QuotaGB 25`
-   **Resetuj hasło:** `.\scripts\reset-password.ps1 -Username "nowyuser" -NewPassword "NoweLepszeHaslo_456"`
-   **Listuj użytkowników:** `.\scripts\list-users.ps1`
-   **Wyloguj użytkownika:** `.\scripts\terminate-sessions.ps1 -Username "nowyuser"`
-   **Pokaż statystyki:** `.\scripts\system-stats.ps1`

---

## Przegląd API Endpoints

Wszystkie ścieżki są poprzedzone `/api/v1`. Wszystkie chronione endpointy wymagają nagłówka `Authorization: Bearer <access_token>`.

**Ważne informacje o odpowiedziach:**
*   Większość endpointów zwraca obiekt `RichNode`, który zawiera pole `permissions`. Może ono przyjąć wartości:
    *   `"owner"`: Jesteś właścicielem tego zasobu.
    *   `"write"`: Masz uprawnienia do zapisu (odziedziczone z udostępnienia).
    *   `"read"`: Masz uprawnienia tylko do odczytu.
    *   `null` (pole nieobecne): Nie jesteś właścicielem i zasób nie jest Ci udostępniony.
*   Wszystkie endpointy listujące obsługują paginację (`?limit=...&offset=...`) oraz sortowanie.

**Sortowanie:** Użyj parametru `?sort=...`, który przyjmuje listę pól oddzielonych przecinkami. Użyj prefiksu `-` dla sortowania malejącego. Dostępne pola: `name`, `size`, `modifiedAt`, `type`.
*Przykład: `?sort=type,-name` (sortuj po typie rosnąco, potem po nazwie malejąco).*

### Autentykacja i Sesje
- `POST /auth/login`: Logowanie.
- `POST /auth/refresh`: Odświeżanie tokena.
- `GET /sessions`: Listowanie aktywnych sesji.
- `POST /sessions/terminate_all`: Wyloguj wszędzie.
- `DELETE /sessions/{sessionId}`: Wyloguj konkretną sesję.

### Zarządzanie Profilem Użytkownika
- `GET /me`: Pobierz pełne informacje o sobie (w tym użycie storage).
- `PATCH /me`: Zaktualizuj profil (np. `display_name`).
- `PATCH /me/password`: Zmień hasło.

### Pliki i Foldery
- `GET /nodes`: Listuj własne pliki/foldery (z paginacją i sortowaniem).
- `POST /nodes/folder`: Stwórz folder.
- `POST /nodes/file`: Wgraj mały plik(i) (simple upload).
- `GET /nodes/archive`: Pobierz archiwum ZIP.
- `GET /nodes/{nodeId}`: Pobierz szczegółowe metadane obiektu (w tym ścieżkę i udostępnienia).
- `GET /nodes/{nodeId}/download`: Pobierz plik.
- `PATCH /nodes/{nodeId}`: Zmień nazwę lub przenieś.
- `DELETE /nodes/{nodeId}`: Przenieś do kosza.
- `POST /nodes/{nodeId}/restore`: Przywróć z kosza (rekurencyjnie).
- `POST /nodes/{nodeId}/copy`: Stwórz głęboką kopię.

### Przesyłanie Dużych Plików (Chunked Upload)
- `POST /nodes/upload/initiate`: Rozpocznij sesję przesyłania.
- `PATCH /nodes/upload/{uploadId}`: Wgraj część pliku.
- `POST /nodes/upload/{uploadId}/complete`: Zakończ przesyłanie.

### Udostępnianie
- `POST /nodes/{nodeId}/share`: Udostępnij plik/folder.
- `GET /shares/incoming/users`: Listuj, kto mi udostępnił.
- `GET /shares/incoming/nodes`: Przeglądaj, co mi udostępniono.
- `GET /shares/incoming/writeable-folders`: Przeglądaj foldery z prawem do zapisu.
- `GET /shares/outgoing/nodes`: Listuj unikalne pliki/foldery, które ja udostępniłem.
- `DELETE /shares/{shareId}`: Cofnij udostępnienie.

### Funkcje Dodatkowe
- `GET /search`: Wyszukaj pliki i foldery.
- `GET /favorites`: Listuj ulubione.
- `POST /nodes/{nodeId}/favorite`: Dodaj do ulubionych.
- `DELETE /nodes/{nodeId}/favorite`: Usuń z ulubionych.
- `GET /trash`: Listuj zawartość kosza.
- `DELETE /trash/purge`: Opróżnij cały kosz.
- `DELETE /trash/{nodeId}`: Trwale usuń pojedynczy element z kosza.

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

### Format Komunikatów

Komunikaty są wysyłane w formacie JSON i mają następującą strukturę:

```json
{
  "event_type": "nazwa_zdarzenia",
  "payload": { "dane_zwiazane_ze_zdarzeniem" }
}
```

### Katalog Zdarzeń i Struktura Payloadów

| Event Type | Opis | Struktura `payload` | Odbiorcy |
| :--- | :--- | :--- | :--- |
| **`node_created`** | Utworzono nowy plik lub folder. | Pełny obiekt `RichNode`. | Twórca, Właściciel folderu nadrzędnego |
| **`nodes_copied`** | Skopiowano jeden lub więcej plików/folderów. | Tablica `[]` pełnych obiektów `RichNode`. | Kopiujący, Właściciel folderu docelowego |
| **`node_updated`** | Zmieniono właściwość węzła (nazwa, rodzic, ulubione). | Pełny, zaktualizowany obiekt `RichNode`. | Osoba modyfikująca, Właściciel |
| **`node_trashed`** | Przeniesiono plik/folder do kosza. | `{ "id", "parent_id" }` | Osoba usuwająca, Właściciel |
| **`nodes_restored`**| Przywrócono jeden lub więcej plików/folderów z kosza. | Tablica `[]` pełnych obiektów `RichNode`. | Właściciel |
| **`node_purged`**| Trwale usunięto element z kosza. | `{ "id" }` | Właściciel |
| **`node_shared_with_you`** | Ktoś udostępnił Ci zasób. | `{ "share_info", "node_info" }` | Tylko Odbiorca udostępnienia |
| **`node_share_created`** | Potwierdzenie, że udostępniłeś zasób. | `{ "share_info", "node_info", "recipient_username" }` | Tylko Udostępniający |
| **`share_revoked_for_you`** | Ktoś cofnął dla Ciebie udostępnienie. | `{ "node_id" }` | Tylko Odbiorca udostępnienia |
| **`node_share_revoked`** | Potwierdzenie, że cofnąłeś udostępnienie. | `{ "share_id", "node_id" }` | Tylko Udostępniający |

---

## Roadmap / Potencjalne Ulepszenia

-   **Wysokie zużycie RAM przy archiwizacji:** Mechanizm tworzenia archiwum ZIP może być nieefektywny przy bardzo dużych strukturach folderów i mógłby zostać zoptymalizowany (streaming).
-   **Natychmiastowe unieważnianie tokenów (Blacklisting):** Obecnie `access token` jest ważny do momentu naturalnego wygaśnięcia. W przyszłości można zaimplementować mechanizm "czarnej listy" do natychmiastowego unieważniania tokenów po wylogowaniu.
-   **Dziennik Audytowy (Audit Log):** Stworzenie oddzielnego, niezmiennego dziennika zdarzeń krytycznych dla bezpieczeństwa i administracji.