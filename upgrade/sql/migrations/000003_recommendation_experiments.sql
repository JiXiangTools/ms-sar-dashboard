-- Prelaunch baseline: immutable IDs, deletion reserves the ID permanently.
CREATE TABLE t_rec_recall (
    recall_id VARCHAR(48) PRIMARY KEY,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE TABLE t_rec_strategy (
    appid BIGINT NOT NULL REFERENCES t_app(id),
    strategy_id VARCHAR(48) NOT NULL CHECK (strategy_id ~ '^[a-z][a-z0-9_]{0,47}$' AND strategy_id <> 'default'),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (appid, strategy_id)
);
CREATE TABLE t_rec_experiment (
    appid BIGINT NOT NULL REFERENCES t_app(id),
    experiment_id VARCHAR(48) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'running', 'ended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (appid, experiment_id),
    CHECK ((status = 'draft' AND started_at IS NULL AND ended_at IS NULL) OR
           (status = 'running' AND started_at IS NOT NULL AND ended_at IS NULL AND deleted_at IS NULL) OR
           (status = 'ended' AND started_at IS NOT NULL AND ended_at >= started_at))
);
CREATE UNIQUE INDEX t_rec_one_running_experiment ON t_rec_experiment(appid) WHERE status = 'running';
-- Latest desired snapshot. A failed Redis write is retried without revision CAS.
CREATE TABLE t_rec_publication (
    appid BIGINT PRIMARY KEY REFERENCES t_app(id),
    pending BOOLEAN NOT NULL DEFAULT TRUE,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX t_rec_pending_publication ON t_rec_publication(appid) WHERE pending;
