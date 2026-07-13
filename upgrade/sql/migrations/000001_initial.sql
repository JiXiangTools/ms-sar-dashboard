CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS t_admin (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(32) NOT NULL UNIQUE,
    nickname VARCHAR(64) NOT NULL DEFAULT '',
    password VARCHAR(255) NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_update_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE SEQUENCE IF NOT EXISTS t_app_id_seq START WITH 100001 INCREMENT BY 1;

CREATE TABLE IF NOT EXISTS t_app (
    id BIGINT PRIMARY KEY DEFAULT nextval('t_app_id_seq'),
    name VARCHAR(128) NOT NULL,
    secret VARCHAR(255) NOT NULL,
    remark TEXT NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_update_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER SEQUENCE t_app_id_seq OWNED BY t_app.id;
ALTER TABLE t_app ALTER COLUMN id SET DEFAULT nextval('t_app_id_seq');

DO $$
DECLARE
    highest_app_id BIGINT;
    sequence_value BIGINT;
BEGIN
    SELECT MAX(id) INTO highest_app_id FROM t_app;
    IF highest_app_id IS NOT NULL THEN
        SELECT last_value INTO sequence_value FROM t_app_id_seq;
        PERFORM setval('t_app_id_seq', GREATEST(highest_app_id, sequence_value), TRUE);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS t_admin_log (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL DEFAULT 0,
    cate VARCHAR(32) NOT NULL,
    type VARCHAR(32) NOT NULL,
    content JSONB NOT NULL DEFAULT '{}'::JSONB,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_log_cate_type_id ON t_admin_log (cate, type, id DESC);
CREATE INDEX IF NOT EXISTS idx_admin_log_admin_id ON t_admin_log (admin_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_app_disabled_id ON t_app (disabled, id DESC);
CREATE INDEX IF NOT EXISTS idx_app_name_search ON t_app USING gin (name gin_trgm_ops);
