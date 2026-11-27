# SpotHub

Веб-платформа с интерактивной картой для скейтеров, самокатчиков, роллеров и BMX-райдеров: споты с оценками безопасности, чат и статус «Я здесь».

## Структура

- `backend/` — API на Go (chi, PostgreSQL + PostGIS, WebSocket)
- `frontend/` — клиент (HTML, CSS, JS, Яндекс.Карты)
- `docs/` — документация API (`api.md`, `websocket.md`)

## Запуск бэкенда

Нужен только Docker (запущенный Docker Desktop).

    docker compose up -d --build
    curl localhost:8080/health

Ответ `ok` значит, что всё работает. API: `http://localhost:8080`.

Остановить: `docker compose down`. Остановить и стереть данные: `docker compose down -v`.

## Запуск фронтенда

Откройте `frontend/` через локальный сервер, а не двойным кликом по файлу (иначе браузер блокирует запросы к API). Например:

    cd frontend
    python3 -m http.server 5500

Страница откроется на `http://localhost:5500`. Бэкенд разрешает запросы с `localhost` и `127.0.0.1` на любом порту.

## Полезные команды для разработки

Новые споты по умолчанию ждут модерации и не видны на карте. Одобрить все:

    docker compose exec db psql -U spothub -d spothub -c "UPDATE spots SET status='approved' WHERE status='pending';"

Сделать пользователя модератором (подставьте email):

    docker compose exec db psql -U spothub -d spothub -c "UPDATE users SET is_moderator=true WHERE email='you@example.com';"

Логи API: `docker compose logs -f api`

## Ветки

`main` не трогаем напрямую. Работаем в ветках (`feature/...`) и вливаем через Pull Request.
