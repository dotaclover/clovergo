-- Global lock password. At most one row; no row means the app is not locked.
CREATE TABLE app_lock (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT    NOT NULL,
    salt          TEXT    NOT NULL,
    iterations    INTEGER NOT NULL,
    updated_at    TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
