# Lista zakupów

Prosta aplikacja webowa w Go do współdzielonej listy zakupów. Na tym etapie nie ma autoryzacji: każdy użytkownik widzi i edytuje tę samą listę.

Frontend jest napisany w HTML, CSS i vanilla JavaScript. Backend używa tylko standard library Go. Dane są zapisywane lokalnie w pliku JSON z mutexem oraz atomowym zapisem przez plik tymczasowy i `rename`.

## Uruchomienie lokalne

```sh
go run ./cmd/server
```

Domyślnie aplikacja słucha na `:8080`, a frontend jest dostępny pod `http://localhost:8080/`.

## Konfiguracja

- `ADDR` - adres serwera HTTP, domyślnie `:8080`.
- `DATA_FILE` - ścieżka do pliku JSON z listą, domyślnie `data/shopping-list.json`.

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

### `GET /api/items`

Zwraca wszystkie pozycje listy.

### `POST /api/items`

Dodaje pozycję.

```json
{
  "name": "Pomidory",
  "note": "1 kg"
}
```

### `PATCH /api/items/{id}`

Aktualizuje pozycję. Można przekazać dowolny zestaw pól:

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

Usuwa wszystkie kupione pozycje.

```json
{"removed":2}
```

## Testy

```sh
gofmt -w ./cmd ./internal
go test ./...
```
