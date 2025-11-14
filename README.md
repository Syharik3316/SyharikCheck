# SyharikCheck

Система мониторинга доступности серверов с распределенными агентами.

## Содержание

- [Быстрый старт](#быстрый-старт)
- [Установка и настройка](#установка-и-настройка)
- [Деплой на FastPanel](#деплой-на-fastpanel)
- [Настройка Redis для удаленных агентов](#настройка-redis-для-удаленных-агентов)
- [Установка агентов](#установка-агентов)
- [Устранение неполадок](#устранение-неполадок)
- [Полезные команды](#полезные-команды)

---

## Быстрый старт

### 1. Подготовка

```bash
# Скопируйте проект на сервер
# Создайте файл с переменными окружения
cp env.prod.example .env.prod
nano .env.prod  # Заполните все переменные, особенно PUBLIC_API_BASE и REACT_APP_API_BASE
```

**Важные переменные:**
- `POSTGRES_PASSWORD` - надежный пароль для PostgreSQL
- `RESULTS_TOKEN` - случайный токен для безопасности
- `PUBLIC_API_BASE` - ваш домен (например: `https://syharikcheck.yourdomain.com`)
- `REACT_APP_API_BASE` - должен совпадать с `PUBLIC_API_BASE`
- `ADMIN_PASS` - надежный пароль для админ панели

### 2. Сборка

```bash
chmod +x build-prod.sh
./build-prod.sh
```

### 3. Настройка FastPanel

1. Создайте сайт в FastPanel
2. В настройках сайта → "Nginx конфигурация" добавьте конфигурацию (см. раздел [Деплой на FastPanel](#деплой-на-fastpanel))
3. Включите SSL в FastPanel

### 4. Запуск

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

### 5. Проверка

```bash
# Проверьте статус
docker compose -f docker-compose.prod.yml --env-file .env.prod ps

# Проверьте логи
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f
```

**Важные моменты:**
- **Порты**: Проект использует порты 18000 (frontend) и 18080 (api) только для локального доступа
- **База данных**: PostgreSQL на порту 15432, Redis на 16379 (только локально)
- **Безопасность**: Все порты привязаны к 127.0.0.1, доступны только локально

---

## Установка и настройка

### Создание файла переменных окружения

```bash
cd /home/username/syharikcheck
cp env.prod.example .env.prod
nano .env.prod
```

Заполните все переменные:
- `POSTGRES_PASSWORD` - надежный пароль для PostgreSQL
- `RESULTS_TOKEN` - случайный токен для безопасности
- `PUBLIC_API_BASE` - ваш домен (например: `https://syharikcheck.yourdomain.com`)
- `REACT_APP_API_BASE` - должен совпадать с `PUBLIC_API_BASE`
- `ADMIN_PASS` - надежный пароль для админ панели

### Сборка проекта

**Вариант 1: Используя скрипт (рекомендуется)**
```bash
chmod +x build-prod.sh
./build-prod.sh
```

**Вариант 2: Используя Makefile**
```bash
make -f Makefile.prod build-prod
```

**Вариант 3: Вручную**
```bash
# Загрузите переменные
export $(cat .env.prod | grep -v '^#' | xargs)

# Соберите frontend
docker build -f Dockerfile.frontend.prod \
  --build-arg REACT_APP_API_BASE="$REACT_APP_API_BASE" \
  -t syharikcheck-frontend:prod .

# Соберите API и agent
docker compose -f docker-compose.prod.yml build
docker build -f Dockerfile.agent -t aeza-agent:latest .
```

---

## Деплой на FastPanel

### 1. Создание сайта в FastPanel

1. Войдите в панель FastPanel
2. Перейдите в "Сайты" → "Создать сайт"
3. Выберите домен (например: `syharikcheck.yourdomain.com`)
4. Выберите PHP версию (не важно, мы не используем PHP)
5. Создайте сайт

### 2. Настройка Nginx конфигурации

В FastPanel перейдите в настройки вашего сайта → "Nginx конфигурация" и добавьте следующую конфигурацию:

```nginx
server {
    listen 80;
    server_name syharikcheck.yourdomain.com;

    # Проксирование API запросов
    location /api/ {
        proxy_pass http://127.0.0.1:18080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket для API
    location /api/ws {
        proxy_pass http://127.0.0.1:18080/api/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400;
    }

    # Статические файлы React (CSS, JS) - ВАЖНО: ПЕРЕД location /
    location /static/ {
        proxy_pass http://127.0.0.1:18000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_valid 200 1y;
        add_header Cache-Control "public, immutable";
    }

    # Другие статические файлы
    location ~* \.(ico|json|txt|map|woff|woff2|eot|ttf|otf)$ {
        proxy_pass http://127.0.0.1:18000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_valid 200 1y;
        add_header Cache-Control "public, immutable";
    }

    # Все остальные запросы
    location / {
        proxy_pass http://127.0.0.1:18000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_redirect off;
    }
}
```

**Важно:** 
- Порядок `location` блоков важен! `/static/` должен быть **ПЕРЕД** `/`
- Удалите все правила, которые пытаются найти файлы локально (например, `root /var/www/...`)

### 3. Настройка SSL (HTTPS)

В FastPanel:
1. Перейдите в настройки сайта
2. Включите SSL
3. Выберите "Let's Encrypt" для автоматического получения сертификата
4. После получения сертификата, обновите конфигурацию Nginx, добавив редирект с HTTP на HTTPS:

```nginx
server {
    listen 80;
    server_name syharikcheck.yourdomain.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name syharikcheck.yourdomain.com;

    ssl_certificate /path/to/certificate.crt;
    ssl_certificate_key /path/to/key.key;

    # ... остальная конфигурация из шага 2 ...
}
```

### 4. Запуск проекта

**Вариант 1: Используя docker compose**
```bash
cd /home/username/syharikcheck
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

**Вариант 2: Используя Makefile**
```bash
make -f Makefile.prod up-prod
```

### 5. Автозапуск при перезагрузке сервера

**Вариант 1: Использовать systemd (рекомендуется)**

Создайте файл `/etc/systemd/system/syharikcheck.service`:

```ini
[Unit]
Description=SyharikCheck Docker Compose
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/home/username/syharikcheck
ExecStart=/usr/bin/docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
ExecStop=/usr/bin/docker compose -f docker-compose.prod.yml --env-file .env.prod down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
```

Активируйте сервис:
```bash
sudo systemctl daemon-reload
sudo systemctl enable syharikcheck.service
sudo systemctl start syharikcheck.service
```

**Вариант 2: Использовать restart: unless-stopped в docker-compose**

Контейнеры уже настроены с `restart: unless-stopped`, поэтому они автоматически запустятся после перезагрузки Docker.

### 6. Обновление проекта

1. Остановите контейнеры:
```bash
# Используя docker compose
docker compose -f docker-compose.prod.yml --env-file .env.prod down

# Или используя Makefile
make -f Makefile.prod down-prod
```

2. Обновите код (через git или вручную)

3. Пересоберите и запустите:
```bash
# Используя скрипт
./build-prod.sh
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d

# Или используя Makefile
make -f Makefile.prod build-prod
make -f Makefile.prod up-prod
```

### 7. Резервное копирование

**База данных PostgreSQL**

**Используя docker compose:**
```bash
# Создать бэкап
docker compose -f docker-compose.prod.yml --env-file .env.prod exec postgres pg_dump -U ${POSTGRES_USER:-postgres} ${POSTGRES_DB:-syharikcheck} > backup_$(date +%Y%m%d_%H%M%S).sql

# Восстановить из бэкапа
docker compose -f docker-compose.prod.yml --env-file .env.prod exec -T postgres psql -U ${POSTGRES_USER:-postgres} ${POSTGRES_DB:-syharikcheck} < backup_20240101_120000.sql
```

**Используя Makefile:**
```bash
# Создать бэкап
make -f Makefile.prod backup-db

# Восстановить из бэкапа
make -f Makefile.prod restore-db FILE=backups/backup_20240101_120000.sql
```

---

## Настройка Redis для удаленных агентов

По умолчанию Redis в `docker-compose.prod.yml` настроен только для локального доступа (`127.0.0.1:16379`). Для работы с удаленными агентами нужно открыть внешний доступ.

### Вариант 1: Открыть порт Redis (рекомендуется с паролем)

1. **Обновите `docker-compose.prod.yml`:**

```yaml
  redis:
    image: public.ecr.aws/docker/library/redis:7-alpine
    ports:
      - "127.0.0.1:16379:6379"  # Локальный доступ
      - "0.0.0.0:6379:6379"      # Внешний доступ
    command: redis-server --requirepass ${REDIS_PASSWORD}
    environment:
      - REDIS_PASSWORD=${REDIS_PASSWORD}
```

2. **Добавьте `REDIS_PASSWORD` в `.env.prod`:**

```bash
REDIS_PASSWORD=your_secure_redis_password_here
```

3. **Обновите `REDIS_PASSWORD` в `docker-compose.prod.yml` для API:**

```yaml
  api:
    environment:
      REDIS_PASSWORD: ${REDIS_PASSWORD}
```

4. **Настройте firewall:**

```bash
# Разрешите подключения к Redis только с определенных IP
sudo ufw allow from <AGENT_IP> to any port 6379
# Или для всех (менее безопасно)
sudo ufw allow 6379/tcp
```

5. **Перезапустите сервисы:**

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod down
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

### Вариант 2: Использовать Nginx stream для проксирования Redis

Добавьте этот блок в `/etc/nginx/nginx.conf` **ПЕРЕД** секцией `http {}`:

```nginx
stream {
    upstream redis_backend {
        server 127.0.0.1:16379;
    }

    server {
        listen 6379;
        proxy_pass redis_backend;
        proxy_timeout 1s;
        proxy_responses 1;
        error_log /var/log/nginx/redis-stream.error.log;
    }
}
```

После добавления выполните:
```bash
nginx -t
systemctl reload nginx
```

### Вариант 3: Использовать другой порт

Если вы хотите использовать другой порт (например, 16379) для внешнего доступа:

1. **Обновите `docker-compose.prod.yml`:**

```yaml
  redis:
    ports:
      - "127.0.0.1:16379:6379"  # Локальный доступ
      - "0.0.0.0:16379:6379"     # Внешний доступ на порту 16379
```

2. **Установите `EXTERNAL_REDIS_PORT` в `.env.prod`:**

```bash
EXTERNAL_REDIS_PORT=16379
```

3. **Перезапустите сервисы**

### Вариант 4: Использовать SSH туннель или VPN

Если вы не хотите открывать Redis напрямую, можно использовать SSH туннель:

```bash
# На сервере с агентом
ssh -L 6379:localhost:16379 user@syharik.online -N
```

И в агенте использовать `REDIS_ADDR=localhost:6379`.

### Проверка

После настройки проверьте доступность Redis:

```bash
# С удаленного сервера (где запущен агент)
nc -zv syharik.online 6379
# или
telnet syharik.online 6379
```

Если Redis требует пароль, проверьте подключение:

```bash
redis-cli -h syharik.online -p 6379 -a your_password ping
```

Должен вернуть `PONG`.

### Безопасность

⚠️ **ВАЖНО:** Открытие Redis для внешнего доступа без пароля - это серьезная угроза безопасности!

Обязательно:
1. Установите сильный пароль для Redis
2. Настройте firewall для ограничения доступа только с IP адресов агентов
3. Рассмотрите использование VPN или SSH туннеля для более безопасного доступа

---

## Установка агентов

### Скрипт install-agent.sh

Скрипт для автоматической установки агента на удаленный сервер.

#### Использование

```bash
bash install-agent.sh <AGENT_NAME> <REGION> <AGENT_TOKEN> [API_BASE] [REDIS_ADDR] [RESULTS_TOKEN]
```

#### Параметры

- `AGENT_NAME` - имя агента (обязательно)
- `REGION` - регион агента (обязательно, например: FR, RU, US)
- `AGENT_TOKEN` - токен агента (обязательно)
- `API_BASE` - базовый URL API (по умолчанию: https://syharik.online)
- `REDIS_ADDR` - адрес Redis сервера (по умолчанию: извлекается из API_BASE)
- `RESULTS_TOKEN` - токен для отправки результатов (по умолчанию: dev-token)

#### Пример

```bash
bash install-agent.sh agent-fr-01 FR abc123def456 https://syharik.online
```

#### Что делает скрипт

1. Устанавливает Docker и необходимые зависимости
2. Очищает старые контейнеры и образы
3. Загружает образ агента (если нужно)
4. Запускает контейнер агента с правильными переменными окружения

#### Размещение скрипта

Скрипт должен быть размещен на сервере по адресу:
`https://syharikhost.ru/uploads/install-agent.sh`

API автоматически формирует команду для загрузки и запуска этого скрипта с правильными параметрами.

---

## Устранение неполадок

### Проблемы с деплоем

#### 404 ошибки для статических файлов (CSS, JS)

**Симптомы:**
В консоли браузера видны ошибки:
```
GET https://syharik.online/static/css/main.8ecdc290.css [HTTP/1.1 404 Not Found]
GET https://syharik.online/static/js/main.677737ec.js [HTTP/1.1 404 Not Found]
```

**Решения:**

1. **Контейнер не был пересобран после изменений:**
```bash
# Остановите контейнеры
docker compose -f docker-compose.prod.yml --env-file .env.prod down

# Пересоберите frontend
docker build -f Dockerfile.frontend.prod \
  --build-arg REACT_APP_API_BASE="https://syharik.online" \
  -t syharikcheck-frontend:prod .

# Запустите снова
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

2. **Проверка содержимого контейнера:**
```bash
# Запустите временный контейнер
docker run --rm -it syharikcheck-frontend:prod sh

# Внутри контейнера проверьте:
ls -la /usr/share/nginx/html/
ls -la /usr/share/nginx/html/static/
ls -la /usr/share/nginx/html/static/css/
ls -la /usr/share/nginx/html/static/js/
```

3. **Проблема с FastPanel проксированием:**
Убедитесь, что в конфигурации FastPanel статические файлы проксируются на frontend контейнер (см. раздел [Деплой на FastPanel](#деплой-на-fastpanel)).

4. **Полная пересборка:**
```bash
# 1. Остановите все
docker compose -f docker-compose.prod.yml --env-file .env.prod down

# 2. Удалите старые образы
docker rmi syharikcheck-frontend:prod || true

# 3. Очистите build кеш
docker builder prune -f

# 4. Пересоберите
./build-prod.sh

# 5. Запустите
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d

# 6. Проверьте логи
docker compose -f docker-compose.prod.yml --env-file .env.prod logs frontend
```

#### WebSocket не работает

**Симптомы:**
WebSocket соединение не устанавливается, ошибки в консоли.

**Решение:**
1. Убедитесь, что в FastPanel правильно настроен WebSocket прокси (см. раздел [Деплой на FastPanel](#деплой-на-fastpanel))
2. Проверьте, что `PUBLIC_API_BASE` использует правильный протокол (https://)
3. Проверьте логи API: `docker compose -f docker-compose.prod.yml --env-file .env.prod logs api`

#### API запросы не работают

**Симптомы:**
Запросы к `/api/` возвращают ошибки.

**Решение:**
1. Проверьте, что API контейнер запущен: `docker compose -f docker-compose.prod.yml --env-file .env.prod ps`
2. Проверьте логи API: `docker compose -f docker-compose.prod.yml --env-file .env.prod logs api`
3. Проверьте, что FastPanel правильно проксирует на порт 18080
4. Проверьте переменные окружения в `.env.prod`

#### Контейнеры не запускаются

**Решение:**
1. Проверьте логи: `docker compose -f docker-compose.prod.yml --env-file .env.prod logs`
2. Проверьте, не заняты ли порты: `netstat -tulpn | grep -E '18000|18080|15432|16379'`
3. Проверьте права доступа к `/var/run/docker.sock` (для API контейнера)

#### Frontend не подключается к API

**Решение:**
1. Убедитесь, что `REACT_APP_API_BASE` в `.env.prod` установлен правильно
2. Проверьте, что FastPanel правильно проксирует запросы
3. Проверьте логи frontend: `docker compose -f docker-compose.prod.yml --env-file .env.prod logs frontend`

### Проблемы с агентами

#### Агент показывает статус "офлайн" после установки

**Диагностика:**

1. **Проверьте логи агента:**
```bash
docker logs <AGENT_NAME>
```

2. **Проверьте переменные окружения:**
```bash
docker exec <AGENT_NAME> env | grep -E "(API_BASE|REDIS_ADDR|AGENT_TOKEN|RESULTS_TOKEN)"
```

3. **Используйте скрипт диагностики:**
```bash
bash scripts/check-agent.sh <AGENT_NAME>
```

**Возможные причины и решения:**

1. **Неправильный API_BASE:**
   - **Симптомы:** В логах видны ошибки подключения к API, `curl: (6) Could not resolve host`, `connection refused`
   - **Решение:** Проверьте, что `API_BASE` указывает на правильный адрес, доступен с удаленного сервера, использует правильный протокол (http:// или https://)
   - **Пример:** `API_BASE=https://syharik.online` или `API_BASE=http://91.204.75.184:18080`

2. **Неправильный REDIS_ADDR:**
   - **Симптомы:** В логах: `BRPOP error: dial tcp: lookup redis`, агент не получает задачи
   - **Решение:** Redis должен быть доступен с удаленного сервера. Используйте внешний IP или домен
   - **Пример:** `REDIS_ADDR=syharik.online:6379` или `REDIS_ADDR=91.204.75.184:16379`
   - **Важно:** Redis должен быть доступен извне (если агент на другом сервере) или через Docker network (если агент на том же сервере)

3. **Неправильный AGENT_TOKEN:**
   - **Симптомы:** В логах API: `agent not found or revoked`, Heartbeat возвращает 401 Unauthorized
   - **Решение:** Убедитесь, что токен агента совпадает с токеном в базе данных
   - Проверьте токен в админ панели
   - Убедитесь, что агент использует правильный токен: `docker exec <AGENT_NAME> sh -c 'echo $AGENT_TOKEN'`

4. **Проблемы с сетью/Firewall:**
   - **Симптомы:** Таймауты при подключении, `connection refused`, `no route to host`
   - **Решение:**
     - Проверьте, что порты открыты: `curl -I https://syharik.online/api/healthz`
     - Проверьте firewall: `sudo ufw status`, `sudo iptables -L -n`
     - Убедитесь, что Redis доступен: `telnet <REDIS_HOST> 6379` или `nc -zv <REDIS_HOST> 6379`

5. **Агент не отправляет heartbeat:**
   - **Симптомы:** Нет ошибок в логах, контейнер работает, но статус "офлайн"
   - **Решение:**
     - Проверьте, что процесс агента работает: `docker exec <AGENT_NAME> ps aux`
     - Проверьте логи на наличие heartbeat: `docker logs <AGENT_NAME> | grep -i heartbeat`
     - Проверьте вручную отправку heartbeat:
```bash
AGENT_TOKEN=$(docker exec <AGENT_NAME> sh -c 'echo $AGENT_TOKEN')
API_BASE=$(docker exec <AGENT_NAME> sh -c 'echo $API_BASE')
curl -X POST "$API_BASE/api/agent/heartbeat" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$AGENT_TOKEN\"}"
```

**Проверка работы heartbeat:**

1. **Проверьте endpoint на API:**
```bash
curl -X POST https://syharik.online/api/agent/heartbeat \
    -H "Content-Type: application/json" \
    -d '{"token":"YOUR_AGENT_TOKEN"}'
```
Должен вернуть `204 No Content` если токен правильный.

2. **Проверьте в базе данных:**
```bash
docker exec -it <POSTGRES_CONTAINER> psql -U postgres -d syharikcheck -c \
    "SELECT name, last_heartbeat, ip FROM agents WHERE name='<AGENT_NAME>';"
```

**Типичные ошибки в логах:**

1. **`dial tcp: lookup api: no such host`**
   - Проблема: API_BASE указывает на внутренний Docker hostname
   - Решение: Используйте внешний адрес (домен или IP)

2. **`connection refused`**
   - Проблема: Порт закрыт или сервис не запущен
   - Решение: Проверьте, что API доступен на указанном адресе

3. **`agent not found or revoked`**
   - Проблема: Неправильный токен или агент удален
   - Решение: Проверьте токен и создайте агента заново если нужно

4. **`BRPOP error: dial tcp: lookup redis`**
   - Проблема: Redis недоступен
   - Решение: Проверьте REDIS_ADDR и доступность Redis

**Быстрая проверка:**

Выполните все команды последовательно:

```bash
# 1. Проверьте контейнер
docker ps -a | grep <AGENT_NAME>

# 2. Проверьте логи
docker logs --tail 50 <AGENT_NAME>

# 3. Проверьте переменные
docker exec <AGENT_NAME> env | grep -E "(API_BASE|REDIS_ADDR|AGENT_TOKEN)"

# 4. Проверьте подключение к API
API_BASE=$(docker exec <AGENT_NAME> sh -c 'echo $API_BASE')
curl -v "$API_BASE/api/healthz"

# 5. Проверьте отправку heartbeat
AGENT_TOKEN=$(docker exec <AGENT_NAME> sh -c 'echo $AGENT_TOKEN')
curl -X POST "$API_BASE/api/agent/heartbeat" \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"$AGENT_TOKEN\"}"
```

**Перезапуск агента с правильными параметрами:**

```bash
# Остановите и удалите
docker stop <AGENT_NAME>
docker rm <AGENT_NAME>

# Запустите с правильными параметрами
docker run -d --restart unless-stopped \
    --name <AGENT_NAME> \
    -e API_BASE=https://syharik.online \
    -e REDIS_ADDR=<REDIS_HOST>:6379 \
    -e RESULTS_TOKEN=<RESULTS_TOKEN> \
    -e REGION=<REGION> \
    -e AGENT_ID=<AGENT_NAME> \
    -e AGENT_TOKEN=<AGENT_TOKEN> \
    aeza-agent:latest

# Проверьте логи
docker logs -f <AGENT_NAME>
```

### Мониторинг

**Проверка здоровья сервисов:**

```bash
# API health check
curl http://127.0.0.1:18080/healthz

# Проверка статуса контейнеров
docker compose -f docker-compose.prod.yml --env-file .env.prod ps
```

---


## Полезные команды

### Используя docker compose

```bash
# Просмотр статуса
docker compose -f docker-compose.prod.yml --env-file .env.prod ps

# Просмотр логов
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f [service_name]

# Перезапуск сервиса
docker compose -f docker-compose.prod.yml --env-file .env.prod restart [service_name]

# Остановка всех сервисов
docker compose -f docker-compose.prod.yml --env-file .env.prod down

# Остановка с удалением volumes (ОСТОРОЖНО - удалит данные БД)
docker compose -f docker-compose.prod.yml --env-file .env.prod down -v

# Просмотр использования ресурсов
docker stats
```

### Используя Makefile

```bash
# Сборка
make -f Makefile.prod build-prod

# Запуск
make -f Makefile.prod up-prod

# Остановка
make -f Makefile.prod down-prod

# Просмотр логов
make -f Makefile.prod logs-prod

# Перезапуск всех сервисов
make -f Makefile.prod restart-prod

# Просмотр статуса
make -f Makefile.prod status-prod

# Создание бэкапа БД
make -f Makefile.prod backup-db

# Восстановление БД
make -f Makefile.prod restore-db FILE=backups/backup_20240101_120000.sql

# Очистка (удаление всех данных - ОСТОРОЖНО!)
make -f Makefile.prod clean-prod
```

### Диагностика

```bash
# Проверка статуса всех контейнеров
docker compose -f docker-compose.prod.yml --env-file .env.prod ps

# Просмотр логов всех сервисов
docker compose -f docker-compose.prod.yml --env-file .env.prod logs

# Просмотр логов конкретного сервиса
docker compose -f docker-compose.prod.yml --env-file .env.prod logs frontend
docker compose -f docker-compose.prod.yml --env-file .env.prod logs api

# Проверка здоровья API
curl http://127.0.0.1:18080/healthz

# Проверка содержимого frontend контейнера
docker run --rm syharikcheck-frontend:prod ls -la /usr/share/nginx/html/

# Проверка nginx конфигурации
docker run --rm syharikcheck-frontend:prod nginx -t

# Проверка сетевых подключений
docker network inspect syharikcheck_network
```

---

## Безопасность

1. **Измените все пароли по умолчанию** в `.env.prod`
2. **Используйте сильные пароли** для PostgreSQL и админ панели
3. **Настройте firewall** - порты 18000, 18080, 15432, 16379 должны быть доступны только локально
4. **Регулярно обновляйте** Docker образы и зависимости
5. **Настройте SSL** для защиты трафика
6. **Для Redis:** Если открываете внешний доступ, обязательно используйте пароль и ограничьте доступ через firewall

---

## Поддержка

При возникновении проблем:
1. Проверьте раздел [Устранение неполадок](#устранение-неполадок)
2. Проверьте логи контейнеров
3. Убедитесь, что все переменные окружения установлены правильно
4. Проверьте настройки FastPanel и Nginx

