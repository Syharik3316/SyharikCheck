#!/bin/bash

# Скрипт для сборки production версии проекта

set -e

echo "🚀 Начинаем сборку production версии..."

# Проверяем наличие .env.prod файла
if [ ! -f .env.prod ]; then
    echo "❌ Файл .env.prod не найден!"
    echo "📝 Создайте файл .env.prod на основе .env.prod.example"
    exit 1
fi

# Загружаем переменные окружения
export $(cat .env.prod | grep -v '^#' | xargs)

# Проверяем обязательные переменные
if [ -z "$REACT_APP_API_BASE" ]; then
    echo "❌ REACT_APP_API_BASE не установлен в .env.prod"
    exit 1
fi

if [ -z "$PUBLIC_API_BASE" ]; then
    echo "❌ PUBLIC_API_BASE не установлен в .env.prod"
    exit 1
fi

echo "✅ Переменные окружения загружены"

# Останавливаем существующие контейнеры
echo "🛑 Останавливаем существующие контейнеры..."
docker compose -f docker-compose.prod.yml down || true

# Собираем образы
echo "🔨 Собираем Docker образы..."

# Собираем frontend с production переменными
echo "📦 Собираем frontend..."
if [ -z "$REACT_APP_API_BASE" ]; then
    echo "⚠️  REACT_APP_API_BASE не установлен, frontend будет использовать относительные пути"
    docker build -f Dockerfile.frontend.prod -t syharikcheck-frontend:prod .
else
    docker build \
        -f Dockerfile.frontend.prod \
        --build-arg REACT_APP_API_BASE="$REACT_APP_API_BASE" \
        -t syharikcheck-frontend:prod \
        .
fi

# Собираем API и другие сервисы
echo "📦 Собираем API и другие сервисы..."
docker compose -f docker-compose.prod.yml build

# Собираем agent
echo "📦 Собираем agent..."
docker build -f Dockerfile.agent -t aeza-agent:latest .

# Сохраняем agent образ в tar (для возможного использования)
echo "💾 Сохраняем agent образ..."
docker save aeza-agent:latest -o aeza-agent.tar

echo "✅ Сборка завершена!"
echo ""
echo "📋 Следующие шаги:"
echo "1. Запустите проект: docker compose -f docker-compose.prod.yml --env-file .env.prod up -d"
echo "2. Настройте FastPanel для проксирования на порты 18000 (frontend) и 18080 (api)"
echo "3. Проверьте логи: docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f"

