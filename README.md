# TripGo — репозиторий для лабораторных работ

HTTP-сервис лабораторных работ курса «Разработка микросервисов на Go». Он создаёт поездки,
отдаёт их по id и завершает. Данные хранятся в PostgreSQL, каждая смена статуса пишется в
`trip_status_history`. В ЛР1 сделаны основная часть и задание со звёздочкой с Docker.

В ЛР2 код разложен по слоям, к нему добавлены тесты, `golangci-lint` и OpenTelemetry. Снаружи
сервис ведёт себя так же, как после ЛР1. Задание со звёздочкой ЛР2 (мутационное тестирование) не
сделано.

## Требования

Go 1.26+, Docker, [`tripgoctl`](https://github.com/course-go-autumn-2026/course-infra), `make`, `curl`
и `jq`, для `make lint` ещё `golangci-lint` v2. Ставить `oapi-codegen`, `goose` и `mockgen` не нужно: Makefile вызывает их через `go tool`.

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

## Архитектура

Пакеты разложены по дереву из ТЗ §4.1. Зависимости направлены внутрь: транспорт и репозиторий
знают про usecase, usecase знает про домен, домен не знает ни про кого.

```
cmd/service             читает конфиг, зовёт container.New, запускает сервер, ждёт сигнала
internal/
  config                переменные окружения
  container             создаёт зависимости и связывает их, здесь же роутер chi
  domain/trip           статусы, переход Finish, проверки New, ошибки домена
  model                 данные, которые ходят между слоями: Trip, NewTrip, StatusChange
  usecase/trip/         по пакету на сценарий: create, get, finish
  http/trip/            по пакету на ручку: create, get, finish
  http/health           /health и /ready
  http/domainerror      ошибка → problem+json, в одном месте
  http/middleware       Recover → Tracing → Logging → Metrics
  repository/trip       SQL через squirrel, транзакция берётся из контекста
  infra/postgres        пул pgx и трейсер запросов; transaction — менеджер транзакций
  infra/telemetry       OpenTelemetry, логгер slog, метрики поездок
  generated             код по OpenAPI
```

Домен объявляет `type Trip model.Trip` с методом `Finish` и конструктором `New`, так что поля
поездки описаны один раз и конвертеров между структурами нет.

Интерфейсы лежат в `contract.go` того пакета, который ими пользуется. Usecase create сам пишет,
что ему нужно от репозитория (`Save` и `AddStatusChange`), ручка create пишет, что ей нужно от
usecase (`Create`). Поэтому интерфейсы узкие и легко мокаются, а реализация про потребителя не
знает. Моки генерирует `mockgen` в `mocks_test.go`, лишние импорты между слоями ловит `depguard`.

Сверх дерева ТЗ добавлен только `http/health`: проб в дереве нет, а ручке нужен свой пакет. Путь
`cmd/service` взят из ТЗ, хотя в `conventions.md` написано `cmd/trip-service`.

## Наблюдаемость

Логи идут в stdout в JSON, внутри запроса в них есть `trace_id` и `span_id`. Трейсы и метрики
уходят по OTLP в коллектор окружения, адреса Jaeger и Grafana печатает `tripgoctl connect`. Если
коллектор недоступен, сервис продолжает работать. Метрики: `http.server.request.duration`,
`http.server.active_requests`, `db.client.operation.duration`, `tripgo.trips.created` и
`tripgo.trips.completed`.

## Переменные окружения

Все, кроме `OTEL_METRIC_EXPORT_INTERVAL`, обязательны: без любой из них сервис не стартует и перечисляет все ошибки сразу. `.env` создаёт
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
| `OTEL_SERVICE_NAME` | имя сервиса в телеметрии и поле `service` в логах |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | адрес коллектора |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | только `http/protobuf` |
| `OTEL_RESOURCE_ATTRIBUTES` | `service.namespace` и `deployment.environment` |
| `OTEL_TRACES_SAMPLER` | сэмплирование трейсов |
| `OTEL_METRIC_EXPORT_INTERVAL` | интервал экспорта метрик в мс, по умолчанию 60000 |

HTTP-таймауты и `OTEL_METRIC_EXPORT_INTERVAL` добавлены сверх переменных курса. Они лежат в секции `[env]` файла
[`environment.toml`](environment.toml), и tripgoctl сам дописывает их в `.env`.

## Команды

```bash
make
make run
make test
make test-integration
make lint
make generate
make migrate
make migrate-down
make docker-run
```

`make test` гоняет unit-тесты с `-race` и в конце печатает покрытие domain + usecase, база ему не
нужна. Тесты репозитория помечены тегом `integration` и запускаются через `make test-integration`
на базе из `DATABASE_URL`, так что сначала нужны `tripgoctl environment start` и `make migrate`.
Каждый такой тест сам создаёт свои данные и удаляет их, поэтому повторный запуск тоже проходит.

## Принятые решения

### Уровень изоляции

Read Committed, задаётся явно при старте транзакции. Его хватает: две
активные поездки запрещает индекс, повторное завершение отсекает блокировка строки (`SELECT … FOR UPDATE`). На Repeatable
Read и Serializable те же конфликты давали бы ошибки `40001`, которые пришлось бы повторять.

### Менеджер транзакций

Менеджер реализует `Do(ctx, fn)`
([`internal/infra/postgres/transaction/manager.go`](internal/infra/postgres/transaction/manager.go)). Он кладёт транзакцию в
контекст, репозиторий берёт её оттуда, а без неё работает через пул. Успешный `fn` завершается
`COMMIT`, ошибка или паника `ROLLBACK`. Вложенный `Do` работает в той же транзакции. Поездка и
запись в `trip_status_history` создаются в одном `Do`. Бизнес-код не импортирует `pgx`.

### Одна активная поездка на водителя

Её обеспечивает частичный уникальный индекс:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS trips_driver_active_uniq ON trips (driver_id) WHERE status = 'active';
```

Второй одновременный `INSERT` ждёт первый и получает ошибку `23505`, которую репозиторий
превращает в `trip.ErrDriverBusy`, а транспорт в `409 driver_busy`.

### Валидация запроса

Тело `POST /api/v1/trips` разбирается в тип с полями-указателями, чтобы отсутствующее поле
отличалось от нуля. Неизвестные поля отклоняются, а диапазоны координат и цену проверяет домен.
Тест сверяет поля этого типа со сгенерированным `api.TripData`. Ключи JSON при этом сравниваются
без учёта регистра, и `"USER_ID"` тоже примется как `user_id`: строгая проверка потребовала бы
второго прохода по телу.

### Телеметрия

Провайдеры OpenTelemetry передаются явно: в `otelhttp`, в трейсер pgx и в метрики. Глобально
задан только обработчик ошибок экспорта, чтобы эти ошибки попадали в наш лог на уровне `Warn`.

## Docker

```bash
make docker-run
curl -s localhost:18080/ready
docker stop trip-service
```

Образ собирается в две стадии: итоговый слой на distroless, внутри только бинарь,
запуск не от root. База остаётся в tripgoctl, контейнер подключается к ней через docker-сеть `kind`.
Телеметрия из контейнера не уходит: адрес коллектора в `.env` указывает на `localhost` хоста. Сам
`docker` Makefile запускает без `OTEL_EXPORTER_OTLP_ENDPOINT`, иначе Docker CLI отправлял бы в
Jaeger свои спаны под именем `trip-service`.

Образ весит 35,3 МБ, в ЛР1 было 20,7 МБ: добавился OpenTelemetry.
