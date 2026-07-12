# Lista zakupów

Prosta aplikacja webowa w Go do współdzielonych, nazwanych list zakupów. Na tym etapie nie ma autoryzacji: każdy użytkownik widzi i edytuje te same listy.

Frontend jest napisany w HTML, CSS i vanilla JavaScript. Backend zapisuje dane w lokalnej bazie SQLite. Każda zmiana dopisuje zdarzenie do append-only tabeli `events`, a aktualny stan list i pozycji jest trzymany w projekcjach `lists` oraz `items` do szybkich odczytów.

Aplikacja obsługuje wiele kontekstów zakupowych, np. `Dom`, `Weekend`, `Grill` albo `Działka`. Każda lista ma własne pozycje, liczniki i operacje dodawania, edycji, oznaczania jako kupione, usuwania oraz czyszczenia kupionych.

Widok aktywnej listy odświeża się automatycznie przez long polling. Gdy ktoś na innym urządzeniu doda, usunie albo zaktualizuje pozycję, zmieni nazwę listy lub wyczyści kupione pozycje, otwarta lista pobierze aktualne metadane i pozycje bez ręcznego odświeżania strony. Wersje odpowiedzi są oparte o identyfikatory zdarzeń SQLite, co daje podstawę pod przyszłą synchronizację offline między urządzeniami.

## Uruchomienie lokalne

```sh
go run ./cmd/server
```

Domyślnie aplikacja słucha na `:8080`, a frontend jest dostępny pod `http://localhost:8080/`.

## Uruchomienie w Docker Compose

```sh
docker compose up -d --build
```

Aplikacja będzie dostępna pod `http://localhost:8080/`. Dane są trzymane w wolumenie `shopping_data`, więc przetrwają restart kontenera.

Przydatne komendy:

```sh
docker compose logs -f
docker compose down        # zatrzymuje kontener, wolumen zostaje
docker compose down -v     # usuwa też dane listy zakupów
```

## Konfiguracja

- `ADDR` - adres serwera HTTP, domyślnie `:8080`.
- `DB_PATH` - ścieżka do pliku SQLite, domyślnie `data/shopping-list.sqlite`.

Przykład:

```sh
ADDR=:3000 DB_PATH=/tmp/lista.sqlite go run ./cmd/server
```

## API

Wszystkie odpowiedzi API są w JSON.

### `GET /healthz`

Sprawdzenie zdrowia aplikacji.

```json
{"status":"ok"}
```

### `GET /api/lists`

Zwraca listy z metadanymi i licznikami.

```json
[
  {
    "id": "abc",
    "name": "Dom",
    "totalCount": 3,
    "openCount": 2,
    "doneCount": 1,
    "version": 12,
    "createdAt": "2026-07-11T12:00:00Z",
    "updatedAt": "2026-07-11T12:00:00Z"
  }
]
```

### `POST /api/lists`

Tworzy listę.

```json
{
  "name": "Weekend"
}
```

### `PATCH /api/lists/{listID}`

Zmienia nazwę listy.

```json
{
  "name": "Dom i ogród"
}
```

### `DELETE /api/lists/{listID}`

Usuwa listę. Aplikacja nie zostawia pustego zbioru list: po usunięciu ostatniej listy automatycznie powstaje domyślna `Lista zakupów`.

### `GET /api/lists/{listID}/items`

Zwraca pozycje wybranej listy oraz aktualną wersję danych.

```json
{
  "items": [
    {
      "id": "item-1",
      "name": "Pomidory",
      "note": "1 kg",
      "completed": false,
      "createdAt": "2026-07-11T12:00:00Z",
      "updatedAt": "2026-07-11T12:00:00Z"
    }
  ],
  "version": 12
}
```

### `GET /api/lists/{listID}/events?since=<version>`

Long-poll endpoint dla aktywnej listy. Jeśli aktualna wersja danych jest większa niż `since`, odpowiada od razu:

```json
{"version":13}
```

Jeśli nie ma zmian, request czeka do około 30 sekund albo do kolejnej mutacji. Po timeout zwraca bieżącą wersję w tym samym formacie. Zerwanie połączenia przez klienta przerywa oczekiwanie po stronie serwera.

### `GET /api/events?since=<version>`

Zwraca surowe zdarzenia z append-only logu po podanej wersji. Endpoint jest pomocniczy pod debugowanie i przyszłą synchronizację offline.

### `POST /api/lists/{listID}/items`

Dodaje pozycję do wybranej listy.

```json
{
  "name": "Pomidory",
  "note": "1 kg"
}
```

### `PATCH /api/lists/{listID}/items/{itemID}`

Aktualizuje pozycję na wybranej liście. Można przekazać dowolny zestaw pól:

```json
{
  "name": "Pomidory koktajlowe",
  "note": "2 opakowania",
  "completed": true
}
```

### `DELETE /api/lists/{listID}/items/{itemID}`

Usuwa pozycję z wybranej listy. Przy powodzeniu zwraca status `204 No Content`.

### `POST /api/lists/{listID}/items/clear-completed`

Usuwa wszystkie kupione pozycje z wybranej listy.

```json
{"removed":2}
```

## Testy

```sh
gofmt -w ./cmd ./internal
go test ./...
```
