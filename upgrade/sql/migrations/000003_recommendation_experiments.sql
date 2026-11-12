-- Immutable definitions and snapshots. Lifecycle timestamps are maintained on
-- experiment versions; the same strategy can be reused by several experiments.
CREATE TABLE t_rec_source_version (
    source_id VARCHAR(48) NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_id, version)
);
CREATE TABLE t_rec_strategy_version (
    appid BIGINT NOT NULL REFERENCES t_app(id),
    strategy_id VARCHAR(10) NOT NULL CHECK (strategy_id ~ '^[a-z][a-z_]{0,9}$' AND strategy_id <> 'default'),
    version BIGINT NOT NULL CHECK (version > 0),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, strategy_id, version)
);
CREATE TABLE t_rec_experiment_version (
    appid BIGINT NOT NULL REFERENCES t_app(id),
    experiment_id VARCHAR(48) NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'running', 'ended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    PRIMARY KEY (appid, experiment_id, version),
    CHECK ((status = 'draft' AND started_at IS NULL AND ended_at IS NULL) OR
           (status = 'running' AND started_at IS NOT NULL AND ended_at IS NULL) OR
           (status = 'ended' AND started_at IS NOT NULL AND ended_at >= started_at))
);
CREATE UNIQUE INDEX t_rec_one_running_experiment ON t_rec_experiment_version(appid) WHERE status = 'running';
-- Transactional publication outbox: revision > published_revision means pending.
CREATE TABLE t_rec_publication (
    appid BIGINT PRIMARY KEY REFERENCES t_app(id),
    revision BIGINT NOT NULL CHECK (revision > 0 AND revision <= 9007199254740991),
    published_revision BIGINT NOT NULL DEFAULT 0,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX t_rec_pending_publication ON t_rec_publication(appid) WHERE revision > published_revision;
