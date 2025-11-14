#!/bin/bash

# Скрипт установки агента на удаленный сервер
# Использование: bash install-agent.sh <AGENT_NAME> <REGION> <AGENT_TOKEN> <API_BASE> <REDIS_ADDR> <RESULTS_TOKEN>

echo -n -e "Nice to meet you! \n My name is Mimic\n"
echo -n -e "Now we'll install requirements\n"

# Параметры
AGENT_NAME=$1
REGION=$2
AGENT_TOKEN=$3
API_BASE=${4:-https://syharik.online}
REDIS_ADDR=${5:-}
RESULTS_TOKEN=${6:-dev-token}

# Извлекаем хост из API_BASE для Redis если не указан
if [ -z "$REDIS_ADDR" ]; then
    # Убираем протокол и путь, оставляем только хост:порт
    REDIS_HOST=$(echo $API_BASE | sed -E 's|https?://||' | sed -E 's|/.*||' | cut -d: -f1)
    REDIS_ADDR="${REDIS_HOST}:6379"
fi

# Установка зависимостей
apt update
apt install curl -y
apt install apt-transport-https ca-certificates curl software-properties-common -y
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu bionic stable" -y
sudo apt update
apt-cache policy docker-ce
apt install docker-ce -y
sleep 5

echo -n -e "Some magic...\n"
docker container prune -f
docker image prune -f

echo -n -e "Download agent.tar...\n"
# Имя файла агента (можно настроить)
AGENT_TAR="aeza-agent.tar"
rm -f $AGENT_TAR

# Загружаем образ агента (если нужно)
# Раскомментируйте следующую строку если образ агента размещен на сервере
# wget https://syharikhost.ru/uploads/$AGENT_TAR --no-check-certificate || echo "Agent tar not found, using local image"

echo -n -e "Load agent.tar (if exists)\n"
if [ -f "$AGENT_TAR" ]; then
    docker load -i $AGENT_TAR
else
    # Если tar файл не найден, пытаемся использовать образ напрямую
    echo "Agent tar not found, trying to use image directly"
    docker pull aeza-agent:latest || echo "Image pull failed, will use local if available"
fi

echo -n -e "Register agent\n"
# Удаляем старый контейнер если существует
docker rm -f $AGENT_NAME 2>/dev/null || true

# Запускаем агента
docker run -d --restart unless-stopped \
    --name $AGENT_NAME \
    -e REDIS_ADDR=$REDIS_ADDR \
    -e API_BASE=$API_BASE \
    -e RESULTS_TOKEN=$RESULTS_TOKEN \
    -e REGION=$REGION \
    -e AGENT_ID=$AGENT_NAME \
    -e AGENT_TOKEN=$AGENT_TOKEN \
    aeza-agent:latest

echo -n -e "Welcome to Family!\n"
echo "Agent $AGENT_NAME started with:"
echo "  Region: $REGION"
echo "  API Base: $API_BASE"
echo "  Redis: $REDIS_ADDR"

