# Go Task Manager

REST API для управления задачами на Go с постоянным хранением данных в PostgreSQL.

## Описание проекта

Учебный backend-проект: создание, получение списка и удаление задач через HTTP API. Каждая задача содержит название, описание и признак выполнения. Данные сохраняются в PostgreSQL и доступны после перезапуска приложения.

В проекте реализованы:

- JSON API на Gin с проверкой входных данных;
- SQL-запросы с параметрами и пул подключений `pgxpool`;
- создание таблицы при запуске приложения;
- таймауты операций с базой и обработка ошибок;
- PostgreSQL в Docker с постоянным томом и проверкой готовности;
- тесты валидации и интеграционный тест с отдельной схемой БД.

## Стек технологий

| Технология | Назначение |
|---|---|
| Go | HTTP-сервер и логика приложения; версия из `go.mod`: 1.27.1 |
| Gin | Маршрутизация, чтение JSON, ответы и журналирование запросов |
| PostgreSQL 15 | Постоянное хранение задач и генерация ID |
| pgx v5 / pgxpool | Драйвер PostgreSQL и пул подключений |
| Docker / Docker Compose | Запуск и настройка PostgreSQL |
| testing / net/http/httptest | Автоматические тесты API |

## Архитектура

```mermaid
flowchart LR
    Client["Клиент / PowerShell"] -->|HTTP / JSON| Gin["Gin :8080"]
    Gin --> Handlers["Обработчики в main.go"]
    Handlers -->|SQL с параметрами| Pool["pgxpool"]
    Pool --> DB[("PostgreSQL 15")]
    DB --- Volume["Docker volume: postgres_data"]
```

Приложение запускается на хосте командой `go run main.go`. Docker Compose запускает базу данных. Обработчики и SQL-запросы находятся в `main.go`.

### Запуск приложения и миграция

1. `main()` вызывает `run()` и выводит ошибку, если запуск не удался.
2. `run()` читает `DATABASE_URL`, создаёт пул подключений и проверяет базу через `Ping`.
3. `migrate()` выполняет `CREATE TABLE IF NOT EXISTS tasks`.
4. `newRouter()` регистрирует маршруты, после чего сервер слушает порт `8080`.

На подключение и миграцию отведено 10 секунд. Каждый HTTP-обработчик ограничивает работу с БД 5 секундами и использует контекст запроса.

Миграция создаёт начальную таблицу; повторный запуск сохраняет записи. Для изменения уже существующей структуры понадобятся дополнительные миграции.

### Модель задачи

| Поле JSON | Тип Go | Тип PostgreSQL | Назначение |
|---|---|---|---|
| `id` | `int` | `INTEGER`, identity, primary key | Назначается базой автоматически |
| `title` | `string` | `TEXT NOT NULL` | Обязательное название |
| `description` | `string` | `TEXT NOT NULL` | Описание, по умолчанию пустая строка |
| `is_completed` | `bool` | `BOOLEAN NOT NULL` | Признак выполнения, по умолчанию `false` |

При создании задачи пробелы по краям названия удаляются. Пустое название отклоняется. Переданный клиентом `id` не используется для назначения ID.

### Роуты

| Метод | Путь | Действие | Успешный ответ | Ошибки |
|---|---|---|---|---|
| GET | `/tasks` | Список задач по возрастанию ID | `200 OK`, JSON-массив | `500` |
| POST | `/tasks` | Создание задачи | `201 Created`, JSON задачи | `400`, `500` |
| DELETE | `/tasks/:id` | Удаление задачи по ID | `204 No Content`, без тела | `400`, `404`, `500` |

Пустой список возвращается как `[]`. Для DELETE допустимы ID от `1` до `2147483647`. При повторном удалении уже удалённой задачи API возвращает `404`.

Ошибки имеют JSON-формат:

```json
{
  "error": "Задача не найдена"
}
```

SQL-запросы передают значения через параметры `$1`, `$2`, `$3`. Подробности ошибок базы записываются в журнал сервера; клиент получает общее сообщение.

### Структура проекта

```text
go-task-manager/
├── main.go             # Подключение к БД, миграция и обработчики API
├── main_test.go        # Тесты входных данных и работы с PostgreSQL
├── docker-compose.yml # PostgreSQL 15, healthcheck и постоянный том
├── go.mod              # Модуль и версии зависимостей
├── go.sum              # Контрольные суммы зависимостей
├── .gitignore          # Локальные настройки и результаты сборки
├── README.md           # Документация проекта
└── INTERVIEW.md        # 20 вопросов для подготовки к собеседованию
```

## Инструкция по запуску

### Требования

- Go 1.27.1 или новее, согласно `go.mod`.
- Docker с Compose v2; на Windows — запущенный Docker Desktop с Linux-контейнерами.
- PowerShell для примеров ниже.
- Свободные порты `5432` для PostgreSQL и `8080` для API.

Выполняйте команды в корне проекта.

### 1. Загрузка зависимостей

```powershell
go mod download
```

### 2. Запуск PostgreSQL

```powershell
docker compose up -d --wait
docker compose ps
```

`--wait` дожидается успешного healthcheck. База доступна на `127.0.0.1:5432`, её файлы находятся в томе `postgres_data`.

### 3. Запуск API

```powershell
go run main.go
```

Адрес API: `http://localhost:8080`. Терминал с сервером остаётся открытым; запросы выполняются в другом окне PowerShell.

### Настройка подключения

При отсутствии переменной `DATABASE_URL` приложение использует локальные параметры из Compose:

```text
postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable
```

Учётные данные и отключённый TLS в этом примере предназначены для локальной разработки. Для другой базы задайте строку подключения перед запуском:

```powershell
$env:DATABASE_URL = "postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
go run main.go
```

Приложение читает переменные окружения процесса. Файл `.env` автоматически не загружается Go-приложением.

### Остановка

Остановите API сочетанием `Ctrl+C`, затем остановите PostgreSQL:

```powershell
docker compose down
```

Обычный `down` сохраняет именованный том. Вариант `down -v` удаляет том вместе с данными.

## Примеры API-запросов: PowerShell

Выполняйте примеры последовательно в одном окне PowerShell. В `-Uri` передаётся обычный URL, без Markdown-оформления ссылок.

### Получить задачи

```powershell
$baseUrl = "http://localhost:8080"
Invoke-RestMethod -Uri "$baseUrl/tasks" -Method Get
```

### Создать задачу

Тело передаётся в UTF-8, чтобы кириллица корректно обрабатывалась и в Windows PowerShell 5.1.

```powershell
$baseUrl = "http://localhost:8080"
$body = @{
    title = "Изучить Go"
    description = "Прочитать документацию"
    is_completed = $false
} | ConvertTo-Json

$created = Invoke-RestMethod `
    -Uri "$baseUrl/tasks" `
    -Method Post `
    -ContentType "application/json; charset=utf-8" `
    -Body ([System.Text.Encoding]::UTF8.GetBytes($body))

$created | ConvertTo-Json
```

Пример ответа с кодом `201 Created`:

```json
{
  "id": 1,
  "title": "Изучить Go",
  "description": "Прочитать документацию",
  "is_completed": false
}
```

Фактический ID назначается базой. Повторный POST создаёт новую запись.

### Удалить созданную задачу

```powershell
Invoke-RestMethod -Uri "$baseUrl/tasks/$($created.id)" -Method Delete
```

Успешный ответ — `204 No Content`, поэтому команда не выводит JSON. Повтор этого DELETE вернёт `404`; PowerShell покажет HTTP-ошибку.

### Проверить валидацию

```powershell
Invoke-RestMethod `
    -Uri "$baseUrl/tasks" `
    -Method Post `
    -ContentType "application/json" `
    -Body '{"title":"   "}'
```

Ожидается `400 Bad Request`: название задачи обязательно.

## Тестирование

Тесты валидации не требуют работающей БД:

```powershell
go test -v -run '^TestRejectInvalidTaskRequests$' ./...
go vet ./...
```

Для полного набора тестов запустите PostgreSQL и задайте отдельную переменную подключения для тестов:

```powershell
docker compose up -d --wait
$env:TEST_DATABASE_URL = "postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
go test -v -count=1 ./...
```

Интеграционный тест создаёт отдельную временную схему и удаляет её после завершения. Учётной записи нужны права на создание схем. Тест проверяет POST, GET, DELETE, назначение ID, повторную миграцию и сохранение записи после закрытия и открытия пула подключений. Существующие задачи приложения остаются в своей схеме.

Без `TEST_DATABASE_URL` интеграционный тест пропускается.

## Материалы для собеседования

[20 вопросов с краткими ответами по Go, SQL, Docker и REST API](INTERVIEW.md).
