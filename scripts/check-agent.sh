#!/bin/bash

# Скрипт для проверки состояния агента
# Использование: bash check-agent.sh <AGENT_NAME>

AGENT_NAME=${1:-"agent-1"}

echo "🔍 Проверка агента: $AGENT_NAME"
echo ""

# Проверка контейнера
echo "📦 Статус контейнера:"
docker ps -a | grep $AGENT_NAME || echo "❌ Контейнер не найден"
echo ""

# Проверка логов
echo "📋 Последние 20 строк логов:"
docker logs --tail 20 $AGENT_NAME 2>&1 || echo "❌ Не удалось получить логи"
echo ""

# Проверка переменных окружения
echo "⚙️  Переменные окружения:"
docker exec $AGENT_NAME env | grep -E "(API_BASE|REDIS_ADDR|AGENT_TOKEN|RESULTS_TOKEN|REGION|AGENT_ID)" || echo "❌ Не удалось получить переменные"
echo ""

# Проверка подключения к API
echo "🌐 Проверка подключения к API:"
API_BASE=$(docker exec $AGENT_NAME sh -c 'echo $API_BASE' 2>/dev/null || echo "")
if [ -n "$API_BASE" ]; then
    echo "  API_BASE: $API_BASE"
    echo "  Проверка heartbeat endpoint:"
    # Исправляем URL если есть порт 8080 (убираем его, так как API через nginx)
    API_URL=$(echo "$API_BASE" | sed 's|:8080||' | sed 's|http://|https://|')
    # Если все еще http, пробуем https
    if [[ "$API_URL" == http://* ]]; then
        API_URL=$(echo "$API_URL" | sed 's|http://|https://|')
    fi
    echo "  Исправленный URL: $API_URL"
    curl -s -k -X POST "$API_URL/api/agent/heartbeat" \
        -H "Content-Type: application/json" \
        -d '{"token":"test"}' \
        -w "\n  HTTP Status: %{http_code}\n" 2>&1 || echo "  ❌ Не удалось подключиться"
else
    echo "  ❌ API_BASE не установлен"
fi
echo ""

# Проверка подключения к Redis
echo "🔴 Проверка подключения к Redis:"
REDIS_ADDR=$(docker exec $AGENT_NAME sh -c 'echo $REDIS_ADDR' 2>/dev/null || echo "")
if [ -n "$REDIS_ADDR" ]; then
    echo "  REDIS_ADDR: $REDIS_ADDR"
    # Пытаемся подключиться (если redis-cli доступен)
    docker exec $AGENT_NAME sh -c "nc -zv ${REDIS_ADDR%%:*} ${REDIS_ADDR##*:} 2>&1" || echo "  ⚠️  Не удалось проверить подключение (nc может быть недоступен)"
else
    echo "  ❌ REDIS_ADDR не установлен"
fi
echo ""

# Проверка процессов внутри контейнера
echo "🔄 Процессы в контейнере:"
docker exec $AGENT_NAME ps aux 2>/dev/null || echo "❌ Не удалось получить процессы"
echo ""

echo "✅ Проверка завершена"

