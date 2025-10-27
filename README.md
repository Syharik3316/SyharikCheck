# SyharikCheck

Многорегиональный мониторинг доступности хостов `(HTTP/DNS/TCP/ICMP/UDP/WHOIS)` с агентами.

Демонстрация решения - [Rutube](https://rutube.ru/video/79b277101d3453f5aefd86761ec2fa82/)

## Стек

- **Backend:** *Go (Gin, pgx, go-redis)*
- **БД:** *PostgreSQL*
- **MQ:** *Redis (очередь задач)*
- **Агенты:** *Go (Redis consumer)*
- **UI:** *React (CRA dev-server в compose) \+ WebSocket*
- **Оркестрация:** *Docker / docker-compose*

## Архитектура

См. `architecture.md` и `potok.md`

## Подготовка VDS

`apt update`

Настройка портов:

```
apt install ufw
ufw allow 22
ufw allow 80
ufw allow 8080
ufw deny 5432
ufw deny 6379
ufw enable
ufw reload
```

Установка docker-ce:

```
apt install apt-transport-https ca-certificates curl software-properties-common
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu bionic stable"
apt-cache policy docker-ce
apt install docker-ce
```

Установка nginx:

```
apt install nginx
systemctl enable nginx
```

## Развёртывание

```bash
# скачать решение со sourcecraft
# перейти в директорию проекта

# сборка и запуск
docker compose build
docker compose up -d

# проверка API
curl http://<SERVER_IP>:8080/healthz
```

## Конфигурация (docker-compose)

Сервис `api` использует переменные окружения:

- `API_PORT=8080` — порт API
- `POSTGRES_DSN=postgres://postgres:postgres@postgres:5432/syharikcheck?sslmode=disable` — строка подключения к БД
- `REDIS_ADDR=redis:6379` — адрес Redis
- `RESULTS_TOKEN=dev-token` — токен приёма результатов от агентов
- `AGENTS_COUNT=3` — ожидаемое число агентов для расчёта прогресса
- `TASK_TTL_SECONDS=90` — TTL задачи
- `ADMIN_USER=admin / ADMIN_PASS=******` — учётка админа (Basic Auth)
- `PUBLIC_API_BASE=http://<SERVER_IP>:8080` — базовый адрес API для генерации команд запуска агентов (отредактируйте его)

Сервис `frontend` использует:

- `REACT_APP_API_BASE=http://<SERVER_IP>:8080` — адрес API для UI (отредактируйте его)

## Использование

(примечание: для работы сервиса нужен хотя бы один настроенный агент (см. Создание агента))

1. **Использование через UI:**

* Перейдите по адресу
- Введите цель `(URL/host[:port])`
- Выберите методы `(HTTP/DNS/TCP/ICMP/UDP/WHOIS)` вручную или с помощью шаблонов
- Нажмите “Проверить”
- Получите результаты онлайн, благодаря WebSocket

2) **Использование с помощью API:**

```bash
# создать задачу
curl -s -X POST http://<SERVER_IP>:8080/api/check \
  -H 'Content-Type: application/json' \
  -d '{"target":"https://example.com","methods":["http","dns","tcp","icmp","udp","whois"]}'

# получить задачу
curl -s http://<SERVER_IP>:8080/api/check/<task_id>
```

3) **Использование админ-панели:**

- Нажмите “Админ” в шапке UI
- Введите логин/пароль *(admin/\*\*\*\*\*\* по умолчанию)*
- Пользуйтесь возможностями: просмотр списка агентов,  создание,  удаление,  получение команды для ручного создания бота

# Создание агента

Перейдите в админ панель и нажмите `Создание агента`

Перед вам всплывёт форма, которую нужно заполнить следующими данными: `имя агента`, `регион расположения сервера`, `ip адрес сервера`, `пользователь(желательно root)`, и `пароль пользователя для подключения по ssh`

После нажатия на кнопку `Создать`, ресурс подключается к указанному серверу по ssh, скачивает и выполняет скрипт создания и привязки агента.

Через некоторое время в метрике должен появится новый агент, который не сразу станет активным.

# Возможные проблемы:

Иногда nginx может занимать порт 80, что приводит к проблемам, для профилактики этого рекомендуем найти эти процессы:\
`netstat -tulpn | grep :80`

и убить

`kill <PID>`

# Дополнительно

Проект выполнен командой 42x САУ

Кейсодатель - <https://aeza.net> \| РКСИ

*(РЕШЕНИЕ ВРЕМЕННО ХОСТИТСЯ ТУТ -* [*http://185.17.0.21/*](http://185.17.0.21/)*)*

Хакатон Осень 2025