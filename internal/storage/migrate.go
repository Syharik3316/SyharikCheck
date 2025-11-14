package storage

import (
    "context"
)

func (p *Postgres) EnsureSchema() error {
    ctx := context.Background()
    _, err := p.pool.Exec(ctx, `
        CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
        CREATE TABLE IF NOT EXISTS agents (
            id UUID PRIMARY KEY,
            name TEXT NOT NULL,
            region TEXT NOT NULL,
            ip TEXT,
            token TEXT NOT NULL,
            revoked BOOLEAN NOT NULL DEFAULT FALSE,
            tasks_completed BIGINT NOT NULL DEFAULT 0,
            last_heartbeat TIMESTAMPTZ,
            created_at TIMESTAMPTZ NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_agents_region ON agents(region);
        CREATE INDEX IF NOT EXISTS idx_agents_heartbeat ON agents(last_heartbeat);
        CREATE TABLE IF NOT EXISTS tasks (
            id UUID PRIMARY KEY,
            target TEXT NOT NULL,
            methods TEXT[] NOT NULL,
            status TEXT NOT NULL,
            expected_results INTEGER NOT NULL DEFAULT 0,
            received_results INTEGER NOT NULL DEFAULT 0,
            deadline TIMESTAMPTZ NULL,
            created_at TIMESTAMPTZ NOT NULL,
            updated_at TIMESTAMPTZ NOT NULL
        );
        CREATE TABLE IF NOT EXISTS results (
            id UUID PRIMARY KEY,
            task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
            agent_id TEXT NOT NULL,
            region TEXT NOT NULL,
            method TEXT NOT NULL,
            success BOOLEAN NOT NULL,
            latency_ms BIGINT NOT NULL,
            status_code INTEGER NOT NULL,
            message TEXT NOT NULL,
            checked_at TIMESTAMPTZ NOT NULL,
            created_at TIMESTAMPTZ NOT NULL,
            details JSONB
        );
        ALTER TABLE results ADD COLUMN IF NOT EXISTS details JSONB;
        CREATE INDEX IF NOT EXISTS idx_results_task_id ON results(task_id);
    `)
    return err
}



