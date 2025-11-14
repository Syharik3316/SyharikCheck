package queue

import (
    "context"
    "encoding/json"
    "time"

    "github.com/google/uuid"
    "github.com/redis/go-redis/v9"
)

const defaultTaskQueueKey = "check_tasks"

func agentQueueKey(agentID string) string { return "check_tasks:" + agentID }

type RedisClient struct {
    client *redis.Client
}

func NewRedisClient(addr, password string, db int) (*RedisClient, error) {
    r := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
    if err := r.Ping(context.Background()).Err(); err != nil {
        return nil, err
    }
    return &RedisClient{client: r}, nil
}

func (r *RedisClient) Close() error { return r.client.Close() }

type TaskJob struct {
    TaskID      uuid.UUID `json:"task_id"`
    Target      string    `json:"target"`
    Methods     []string  `json:"methods"`
    RequestedAt time.Time `json:"requested_at"`
}

func (r *RedisClient) EnqueueTask(ctx context.Context, job TaskJob) error {
    b, err := json.Marshal(job)
    if err != nil {
        return err
    }
    return r.client.LPush(ctx, defaultTaskQueueKey, string(b)).Err()
}

// FanOutTask pushes job into per-agent queues so каждый агент получает свою копию.
func (r *RedisClient) FanOutTask(ctx context.Context, agentIDs []string, job TaskJob) error {
    b, err := json.Marshal(job)
    if err != nil { return err }
    for _, id := range agentIDs {
        key := agentQueueKey(id)
        if err := r.client.LPush(ctx, key, string(b)).Err(); err != nil { return err }
    }
    return nil
}




