#!/bin/bash

# Скрипт для проверки содержимого собранного frontend контейнера

echo "🔍 Проверка содержимого frontend контейнера..."

# Проверяем, существует ли образ
if ! docker image inspect syharikcheck-frontend:prod &> /dev/null; then
    echo "❌ Образ syharikcheck-frontend:prod не найден!"
    echo "💡 Сначала соберите проект: ./build-prod.sh"
    exit 1
fi

# Создаем временный контейнер для проверки
echo "📦 Создаю временный контейнер для проверки..."
CONTAINER_ID=$(docker create syharikcheck-frontend:prod)

echo ""
echo "📁 Содержимое /usr/share/nginx/html:"
docker cp $CONTAINER_ID:/usr/share/nginx/html - | tar -t | head -20

echo ""
echo "📁 Содержимое /usr/share/nginx/html/static (если существует):"
docker exec $CONTAINER_ID ls -la /usr/share/nginx/html/static 2>/dev/null || echo "Папка static не найдена"

echo ""
echo "📄 Проверка index.html:"
docker cp $CONTAINER_ID:/usr/share/nginx/html/index.html - | head -30

# Удаляем временный контейнер
docker rm $CONTAINER_ID > /dev/null

echo ""
echo "✅ Проверка завершена"

