package storage

import (
    "context"
    "errors"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
    pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
    cfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, err
    }
    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, err
    }
    return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
    p.pool.Close()
}

type TaskStatus string

const (
    TaskStatusQueued   TaskStatus = "queued"
    TaskStatusRunning  TaskStatus = "running"
    TaskStatusFinished TaskStatus = "finished"
    TaskStatusFailed   TaskStatus = "failed"
)

type CheckTask struct {
    ID        uuid.UUID
    Target    string
    Methods   []string
    Status    TaskStatus
    ExpectedResults int
    ReceivedResults int
    Deadline  *time.Time
    CreatedAt time.Time
    UpdatedAt time.Time
}

type CheckResult struct {
    ID          uuid.UUID `json:"id"`
    TaskID      uuid.UUID `json:"task_id"`
    AgentID     string    `json:"agent_id"`
    Region      string    `json:"region"`
    Method      string    `json:"method"`
    Success     bool      `json:"success"`
    LatencyMs   int64     `json:"latency_ms"`
    StatusCode  int       `json:"status_code"`
    Message     string    `json:"message"`
    CheckedAt   time.Time `json:"checked_at"`
    CreatedAt   time.Time `json:"created_at"`
    Details     any       `json:"details"`
}

type Agent struct {
    ID             uuid.UUID
    Name           string
    Region         string
    IP             string
    Token          string
    Revoked        bool
    TasksCompleted int64
    LastHeartbeat  *time.Time
    CreatedAt      time.Time
}

func (p *Postgres) CountActiveAgents(ctx context.Context) (int, error) {
    row := p.pool.QueryRow(ctx, `SELECT COUNT(1) FROM agents WHERE revoked=FALSE`)
    var n int
    if err := row.Scan(&n); err != nil { return 0, err }
    return n, nil
}

func (p *Postgres) CreateAgent(ctx context.Context, a *Agent) error {
    a.ID = uuid.New()
    a.CreatedAt = time.Now().UTC()
    _, err := p.pool.Exec(ctx, `
        INSERT INTO agents (id, name, region, ip, token, revoked, tasks_completed, last_heartbeat, created_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
    `, a.ID, a.Name, a.Region, a.IP, a.Token, a.Revoked, a.TasksCompleted, a.LastHeartbeat, a.CreatedAt)
    return err
}

func (p *Postgres) ListAgents(ctx context.Context) ([]Agent, error) {
    rows, err := p.pool.Query(ctx, `
        SELECT a.id, a.name, a.region, a.ip, a.token, a.revoked,
               COALESCE((SELECT COUNT(DISTINCT r.task_id) FROM results r WHERE r.agent_id = a.name), 0) AS tasks_completed,
               a.last_heartbeat, a.created_at
        FROM agents a
        WHERE a.revoked = FALSE
        ORDER BY a.created_at DESC
    `)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []Agent
    for rows.Next() {
        var a Agent
        if err := rows.Scan(&a.ID, &a.Name, &a.Region, &a.IP, &a.Token, &a.Revoked, &a.TasksCompleted, &a.LastHeartbeat, &a.CreatedAt); err != nil {
            return nil, err
        }
        out = append(out, a)
    }
    return out, rows.Err()
}

func (p *Postgres) RevokeAgent(ctx context.Context, id uuid.UUID) error {
    ct, err := p.pool.Exec(ctx, `UPDATE agents SET revoked=TRUE WHERE id=$1`, id)
    if err != nil { return err }
    if ct.RowsAffected() == 0 { return errors.New("agent not found") }
    return nil
}

func (p *Postgres) UpdateHeartbeat(ctx context.Context, identifier, ip string) error {
    ct, err := p.pool.Exec(ctx, `
        UPDATE agents SET last_heartbeat=NOW(), ip=$2 
        WHERE (token=$1 OR name=$1) AND revoked=FALSE
    `, identifier, ip)
    if err != nil { return err }
    if ct.RowsAffected() == 0 { return errors.New("agent not found or revoked") }
    return nil
}

func (p *Postgres) CountAgents(ctx context.Context) (int, error) {
    row := p.pool.QueryRow(ctx, `SELECT COUNT(1) FROM agents`)
    var n int
    if err := row.Scan(&n); err != nil { return 0, err }
    return n, nil
}

func (p *Postgres) InsertTask(ctx context.Context, t *CheckTask) error {
    t.ID = uuid.New()
    t.Status = TaskStatusQueued
    now := time.Now().UTC()
    t.CreatedAt = now
    t.UpdatedAt = now
    _, err := p.pool.Exec(ctx, `
        INSERT INTO tasks (id, target, methods, status, expected_results, received_results, deadline, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, t.ID, t.Target, t.Methods, t.Status, t.ExpectedResults, t.ReceivedResults, t.Deadline, t.CreatedAt, t.UpdatedAt)
    return err
}

func (p *Postgres) GetTask(ctx context.Context, id uuid.UUID) (*CheckTask, error) {
    row := p.pool.QueryRow(ctx, `
        SELECT id, target, methods, status, expected_results, received_results, deadline, created_at, updated_at
        FROM tasks WHERE id=$1
    `, id)
    var t CheckTask
    if err := row.Scan(&t.ID, &t.Target, &t.Methods, &t.Status, &t.ExpectedResults, &t.ReceivedResults, &t.Deadline, &t.CreatedAt, &t.UpdatedAt); err != nil {
        return nil, err
    }
    return &t, nil
}

func (p *Postgres) UpdateTaskStatus(ctx context.Context, id uuid.UUID, status TaskStatus) error {
    ct, err := p.pool.Exec(ctx, `
        UPDATE tasks SET status=$2, updated_at=NOW() WHERE id=$1
    `, id, status)
    if err != nil {
        return err
    }
    if ct.RowsAffected() == 0 {
        return errors.New("task not found")
    }
    return nil
}

// ListExpiredRunningTasks returns tasks that are still running but past their deadline.
func (p *Postgres) ListExpiredRunningTasks(ctx context.Context) ([]CheckTask, error) {
    rows, err := p.pool.Query(ctx, `
        SELECT id, target, methods, status, expected_results, received_results, deadline, created_at, updated_at
        FROM tasks
        WHERE status = 'running' AND deadline IS NOT NULL AND deadline < NOW()
        ORDER BY created_at ASC
    `)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []CheckTask
    for rows.Next() {
        var t CheckTask
        if err := rows.Scan(&t.ID, &t.Target, &t.Methods, &t.Status, &t.ExpectedResults, &t.ReceivedResults, &t.Deadline, &t.CreatedAt, &t.UpdatedAt); err != nil {
            return nil, err
        }
        out = append(out, t)
    }
    return out, rows.Err()
}

func (p *Postgres) IncrementReceived(ctx context.Context, id uuid.UUID) (int, int, error) {
    row := p.pool.QueryRow(ctx, `
        UPDATE tasks
        SET received_results = received_results + 1, updated_at = NOW()
        WHERE id=$1
        RETURNING expected_results, received_results
    `, id)
    var exp, rec int
    if err := row.Scan(&exp, &rec); err != nil {
        return 0, 0, err
    }
    return exp, rec, nil
}

func (p *Postgres) SetExpectedAndDeadline(ctx context.Context, id uuid.UUID, expected int, deadline time.Time) error {
    ct, err := p.pool.Exec(ctx, `
        UPDATE tasks SET expected_results=$2, deadline=$3, updated_at=NOW() WHERE id=$1
    `, id, expected, deadline)
    if err != nil {
        return err
    }
    if ct.RowsAffected() == 0 {
        return errors.New("task not found")
    }
    return nil
}

func (p *Postgres) InsertResult(ctx context.Context, r *CheckResult) error {
    r.ID = uuid.New()
    now := time.Now().UTC()
    r.CreatedAt = now
    _, err := p.pool.Exec(ctx, `
        INSERT INTO results (id, task_id, agent_id, region, method, success, latency_ms, status_code, message, checked_at, created_at, details)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
    `, r.ID, r.TaskID, r.AgentID, r.Region, r.Method, r.Success, r.LatencyMs, r.StatusCode, r.Message, r.CheckedAt, r.CreatedAt, r.Details)
    return err
}

func (p *Postgres) ListResultsByTask(ctx context.Context, taskID uuid.UUID) ([]CheckResult, error) {
    rows, err := p.pool.Query(ctx, `
        SELECT id, task_id, agent_id, region, method, success, latency_ms, status_code, message, checked_at, created_at, details
        FROM results WHERE task_id=$1 ORDER BY created_at ASC
    `, taskID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []CheckResult
    for rows.Next() {
        var r CheckResult
        if err := rows.Scan(&r.ID, &r.TaskID, &r.AgentID, &r.Region, &r.Method, &r.Success, &r.LatencyMs, &r.StatusCode, &r.Message, &r.CheckedAt, &r.CreatedAt, &r.Details); err != nil {
            return nil, err
        }
        out = append(out, r)
    }
    return out, rows.Err()
}



