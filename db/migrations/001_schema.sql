-- =============================================================
-- Schema: mr_reviewer_app
-- Dedicated schema for the PR Reviewer service.
-- Running this file on a fresh Postgres instance is idempotent.
-- =============================================================

CREATE SCHEMA IF NOT EXISTS mr_reviewer_app;

-- Set default search path for this session so table names are unqualified below.
SET search_path TO mr_reviewer_app;

-- ---------------------------------------------------------
-- users: lightweight user registry
-- ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    user_id     TEXT        PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------
-- user_tokens: one encrypted GitLab PAT per user
-- The token is encrypted with AES-256-GCM before being stored.
-- ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_tokens (
    user_id         TEXT        PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
    encrypted_token TEXT        NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------
-- review_logs: audit trail — every MR review is recorded
-- ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS review_logs (
    id            BIGSERIAL   PRIMARY KEY,
    user_id       TEXT        NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    mr_id         INT         NOT NULL,
    project_id    TEXT        NOT NULL,
    comment       TEXT        NOT NULL,
    reviewed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_review_logs_user_id ON review_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_review_logs_mr_id   ON review_logs (mr_id);
