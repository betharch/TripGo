# TripGo — репозиторий для лабораторных работ

HTTP-сервис первой лабораторной работы курса «Разработка микросервисов на Go». Он создаёт поездки,
отдаёт их по id и завершает. Данные хранятся в PostgreSQL, каждая смена статуса пишется в
`trip_status_history`. Сделаны основная часть и задание со звёздочкой с Docker.

## Требования

Go 1.26+, Docker, [`tripgoctl`](https://github.com/course-go-autumn-2026/course-infra), `make`, `curl`
и `jq`. Ставить `oapi-codegen` и `goose` не нужно: Makefile вызывает их через `go tool`.

## Запуск

```bash
tripgoctl cluster start
tripgoctl environment start
make migrate
make run
```

Во втором терминале:

```bash
curl -s localhost:8080/ready   # {"status":"ok"}

TRIP_ID=$(curl -s -X POST localhost:8080/api/v1/trips -H 'Content-Type: application/json' -d '{
  "user_id": "5cb72c04-7650-45c9-a79b-bcdba0631e0c",
  "driver_id": "8860b315-ec86-42eb-a17c-7c163d721ff5",
  "start_point": {"latitude": 59.9398, "longitude": 30.3146},
  "end_point": {"latitude": 59.9290, "longitude": 30.3626},
  "price": 1450}' | jq -r .id)
curl -s localhost:8080/api/v1/trips/$TRIP_ID | jq             # 200, status active
curl -s -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish | jq   # 200, status completed
curl -s -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish | jq   # 409 trip_completed
```

Сервис останавливается по Ctrl+C. `tripgoctl environment stop` останавливает PostgreSQL, данные
сохраняются.

## HTTP API

Контракт: [`contracts/openapi/trip-service.openapi.yaml`](contracts/openapi/trip-service.openapi.yaml).

| Метод | Путь | Ответы |
|---|---|---|
| `POST` | `/api/v1/trips` | `201` и `Location`, `400`, `409 driver_busy`, `500` |
| `GET` | `/api/v1/trips/{tripId}` | `200`, `400`, `404 trip_not_found`, `500` |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`, `400`, `404 trip_not_found`, `409 trip_completed`, `500` |
| `GET` | `/health` | `200`, в базу не ходит |
| `GET` | `/ready` | `200` или `503`, если база недоступна |

Ошибки отдаются в `application/problem+json` (RFC 9457) с полем `code`. Заголовок
`Idempotency-Key` не используется, но если он передан, это должен быть UUID.

## Переменные окружения

Все обязательны: без любой из них сервис не стартует и перечисляет все ошибки сразу. `.env` создаёт
tripgoctl, примеры значений лежат в [`.env.example`](.env.example).

| Переменная | Назначение |
|---|---|
| `HTTP_ADDR` | адрес HTTP-сервера |
| `LOG_LEVEL` | `debug`, `info`, `warn` или `error` |
| `SHUTDOWN_TIMEOUT` | бюджет graceful shutdown |
| `DATABASE_URL` | подключение к PostgreSQL |
| `DATABASE_MAX_CONNS`, `DATABASE_MIN_CONNS` | размер пула |
| `DATABASE_MAX_CONN_LIFETIME` | время жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | таймаут подключения |
| `DATABASE_QUERY_TIMEOUT` | таймаут запроса к БД и проверки `/ready` |
| `HTTP_READ_HEADER_TIMEOUT`, `HTTP_READ_TIMEOUT`, `HTTP_WRITE_TIMEOUT`, `HTTP_IDLE_TIMEOUT` | таймауты HTTP-сервера |

HTTP-таймауты добавлены сверх переменных курса. Они лежат в секции `[env]` файла
[`environment.toml`](environment.toml), и tripgoctl сам дописывает их в `.env`.

## Команды

```bash
make
make run
make test
make generate
make migrate
make migrate-down
make docker-run
```

## Принятые решения

### Уровень изоляции

Read Committed, задаётся явно при старте транзакции. Его хватает: две
активные поездки запрещает индекс, повторное завершение отсекает условие в `UPDATE`. На Repeatable
Read и Serializable те же конфликты давали бы ошибки `40001`, которые пришлось бы повторять.

### Менеджер транзакций

Менеджер реализует `Do(ctx, fn)`
([`internal/postgres/txmanager.go`](internal/postgres/txmanager.go)). Он кладёт транзакцию в
контекст, репозиторий берёт её оттуда, а без неё работает через пул. Успешный `fn` завершается
`COMMIT`, ошибка или паника `ROLLBACK`. Вложенный `Do` работает в той же транзакции. Поездка и
запись в `trip_status_history` создаются в одном `Do`. Бизнес-код не импортирует `pgx`.

### Одна активная поездка на водителя

Её обеспечивает частичный уникальный индекс:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS trips_driver_active_uniq ON trips (driver_id) WHERE status = 'active';
```

Второй одновременный `INSERT` ждёт первый и получает ошибку `23505`, которую репозиторий
превращает в `409 driver_busy`. Завершение защищено так же: `UPDATE ... WHERE status = 'active'`
обновляет строку один раз.

## Docker

```bash
make docker-run
curl -s localhost:18080/ready
docker stop trip-service
```

Образ собирается в две стадии: итоговый слой на distroless, внутри только бинарь,
запуск не от root. База остаётся в tripgoctl, контейнер подключается к ней через docker-сеть `kind`.

Образ весит 20,7 МБ.
