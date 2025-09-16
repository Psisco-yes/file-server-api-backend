# Go File Server

## Opis Projektu

W pełni funkcjonalny, REST-owy serwer plików zbudowany w Go, inspirowany systemami takimi jak Google Drive.

## Kluczowe Funkcjonalności

- **Zarządzanie Plikami i Folderami:** Pełen CRUD (tworzenie, listowanie, zmiana nazwy, przenoszenie).
- **Bezpieczeństwo:** Autentykacja oparta na JWT, obsługa HTTPS.
- **Udostępnianie:** Możliwość udostępniania plików i folderów innym użytkownikom.
- **Funkcje UX:** Kosz, ulubione, pobieranie wielu plików jako archiwum ZIP.
- **System Czasu Rzeczywistego:**
  - **Dziennik Zdarzeń:** Umożliwia wydajną synchronizację dla klientów offline.
  - **WebSockets:** Natychmiastowe powiadomienia o zmianach.
- **Monitoring:** Endpointy `/health` i `/metrics` (Prometheus).
- **Dokumentacja API:** Automatycznie generowana dokumentacja Swagger UI.
- **Pełne Testy:** Pokrycie testami jednostkowymi, integracyjnymi i E2E (Postman).

## Stack Technologiczny

- **Backend:** Go (Golang)
- **Baza Danych:** PostgreSQL
- **Reverse Proxy (HTTPS):** Caddy
- **Konteneryzacja:** Docker & Docker Compose
- **Testowanie:** `testcontainers-go`, `testify`
- **Dokumentacja:** `swaggo`

## Uruchomienie

1.  Sklonuj repozytorium
2.  Przejdź do folderu projektu: `cd go-file-server`
3.  Stwórz plik `.env` na podstawie `env.example` i uzupełnij sekrety.
4.  Wygeneruj lokalne certyfikaty za pomocą `mkcert` w folderze `certs`.
5.  Uruchom projekt: `docker-compose up --build`