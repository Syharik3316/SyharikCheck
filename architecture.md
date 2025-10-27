# Архитектура системы мониторинга

```mermaid
flowchart TD
    subgraph MAIN_SERVER["Главный сервер - Москва (79.137.192.74)"]
        subgraph DOCKER_MAIN["Docker Compose - Основные сервисы"]
            REACT[frontend<br/>react:latest<br/>порт 80:80]
            API[backend<br/>go-api:latest<br/>порт 8080:8080]
            POSTGRES[postgres<br/>postgres:15<br/>порт 5432:5432]
            REDIS[redis<br/>redis:7-alpine<br/>порт 6379:6379]
            NGINX[nginx<br/>nginx:alpine<br/>порт 80:80, 443:443]
        end
        
        REACT --> NGINX
        API --> POSTGRES
        API --> REDIS
        NGINX --> REACT
        NGINX --> API
    end

    subgraph AGENT_FRANCE["🇫🇷 Франция (89.208.113.253)"]
        subgraph DOCKER_FR["Docker Agent"]
            AGENT_FR[agent<br/>checkhost-agent:latest<br/>сеть: host]
        end
    end

    subgraph AGENT_AUSTRIA["🇦🇹 Австрия (94.228.170.202)"]
        subgraph DOCKER_AT["Docker Agent"]
            AGENT_AT[agent<br/>checkhost-agent:latest<br/>сеть: host]
        end
    end

    subgraph AGENT_MOSCOW["🇷🇺 Москва агент (77.110.104.9)"]
        subgraph DOCKER_RU["Docker Agent"]
            AGENT_RU[agent<br/>checkhost-agent:latest<br/>сеть: host]
        end
    end

    %% Взаимодействия
    REDIS --> AGENT_FR
    REDIS --> AGENT_AT
    REDIS --> AGENT_RU

    AGENT_FR --> TARGETS[Интернет-хосты<br/>сайты · API · серверы]
    AGENT_AT --> TARGETS
    AGENT_RU --> TARGETS

    AGENT_FR -->|POST /api/results| API
    AGENT_AT -->|POST /api/results| API
    AGENT_RU -->|POST /api/results| API

    USER[Пользователь] --> NGINX