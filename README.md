# Lista zakupów

Prosta aplikacja webowa w Go do współdzielonych, nazwanych list zakupów. Na tym etapie nie ma autoryzacji: każdy użytkownik widzi i edytuje te same listy.

Frontend jest napisany w HTML, CSS i vanilla JavaScript. Backend używa tylko standard library Go. Dane są zapisywane lokalnie w pliku JSON z mutexem oraz atomowym zapisem przez plik tymczasowy i `rename`.

Aplikacja obsługuje wiele kontekstów zakupowych, np. `Dom`, `Weekend`, `Grill` albo `Działka`. Każda lista ma własne pozycje, liczniki i operacje dodawania, edycji, oznaczania jako kupione, usuwania oraz czyszczenia kupionych.

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
- `DATA_FILE` - ścieżka do pliku JSON z listami, domyślnie `data/shopping-list.json`.

Przykład:

```sh
ADDR=:3000 DATA_FILE=/tmp/lista.json go run ./cmd/server
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

Zwraca pozycje wybranej listy.

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

## Kompatybilne endpointy

Endpointy `/api/items` nadal działają na pierwszej/domyślnej liście, ale frontend używa nowych endpointów per lista.

### `GET /api/items`

Zwraca pozycje pierwszej/domyślnej listy.

### `POST /api/items`

Dodaje pozycję do pierwszej/domyślnej listy.

```json
{
  "name": "Pomidory",
  "note": "1 kg"
}
```

### `PATCH /api/items/{id}`

Aktualizuje pozycję z pierwszej/domyślnej listy. Można przekazać dowolny zestaw pól:

```json
{
  "name": "Pomidory koktajlowe",
  "note": "2 opakowania",
  "completed": true
}
```

### `DELETE /api/items/{id}`

Usuwa pozycję. Przy powodzeniu zwraca status `204 No Content`.

### `POST /api/items/clear-completed`

Usuwa wszystkie kupione pozycje z pierwszej/domyślnej listy.

```json
{"removed":2}
```

## Testy

```sh
gofmt -w ./cmd ./internal
go test ./...
```
