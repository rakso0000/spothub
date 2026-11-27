# WebSocket: чат и «Я здесь»

Подключение: `ws://localhost:8080/ws/spots/{id}?token=<JWT>`

Одно соединение на открытую карточку спота. Закрыли карточку: закройте соединение.
Браузер не умеет ставить заголовки на WebSocket, поэтому токен передаётся в адресе.

    const ws = new WebSocket(`ws://localhost:8080/ws/spots/${id}?token=${token}`);
    ws.onmessage = (e) => {
      const ev = JSON.parse(e.data);
      // ev.type: "message" | "checkin" | "checkout" | "message_hidden" | "error"
    };
    ws.send(JSON.stringify({ type: "message", text: "Кто катает?" }));

## Клиент отправляет

    {"type":"message","text":"..."}

Текст 1-500 символов, не чаще 1 сообщения в секунду.

## Сервер присылает

Сообщение чата (всем в комнате, включая отправителя):

    {"type":"message","id":1,"spot_id":1,"user_id":2,"user":"vasya","text":"...","ts":"2026-11-27T20:00:00Z"}

Человек отметился (событие приходит и при продлении, не дублируйте человека в списке, ключ `user_id`):

    {"type":"checkin","spot_id":1,"user_id":2,"user":"vasya","ts":"...","expires_at":"..."}

Человек ушёл или отметка истекла:

    {"type":"checkout","spot_id":1,"user_id":2,"user":"vasya","ts":"..."}

Сообщение скрыто модерацией, уберите его из чата по `id`:

    {"type":"message_hidden","id":5,"spot_id":1}

Ошибка (только отправителю):

    {"type":"error","error":"текст"}

## Как собрать экран спота

1. Открыли карточку: `GET /api/spots/{id}`, `GET /api/spots/{id}/here`, `GET /api/spots/{id}/messages` (список развернуть, чтобы старые были сверху).
2. Подключили WebSocket.
3. Дальше обновляйте список «кто здесь» и чат по событиям, без повторных запросов.
4. При обрыве соединения переподключитесь и ещё раз загрузите `here` и историю, чтобы не потерять события.
