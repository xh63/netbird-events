-- netbird-events lab: SQLite checkpoint table
-- Run against any SQLite store to enable eventsproc checkpoint tracking
-- Safe to run on an existing NetBird store.db (IF NOT EXISTS)

CREATE TABLE IF NOT EXISTS event_processing_checkpoint (
    consumer_id TEXT PRIMARY KEY,
    writer_type TEXT NOT NULL DEFAULT 'default',
    last_event_id INTEGER NOT NULL DEFAULT 0,
    last_event_timestamp DATETIME,
    total_events_processed INTEGER NOT NULL DEFAULT 0,
    processing_node TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
