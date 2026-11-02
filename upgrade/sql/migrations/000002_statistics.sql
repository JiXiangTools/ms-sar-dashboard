-- Only aggregate snapshots are stored here. Raw events stay in alg-rec-offline.
CREATE TABLE t_app_statistics_day (
    appid BIGINT NOT NULL CHECK (appid > 0),
    day DATE NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, day)
);

CREATE TABLE t_app_statistics_snapshot (
    appid BIGINT PRIMARY KEY CHECK (appid > 0),
    revision BIGINT NOT NULL CHECK (revision > 0),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
