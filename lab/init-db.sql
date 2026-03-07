-- netbird-events lab: PostgreSQL initialization script
-- Auto-run on first container start via docker-entrypoint-initdb.d

CREATE SCHEMA IF NOT EXISTS idp;

-- Single-checkpoint model (migration 003+)
-- consumer_id is the sole primary key; writer_type kept for legacy compatibility
CREATE TABLE IF NOT EXISTS idp.event_processing_checkpoint (
    consumer_id VARCHAR(255) PRIMARY KEY,
    writer_type VARCHAR(50) NOT NULL DEFAULT 'default',
    last_event_id BIGINT NOT NULL,
    last_event_timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    total_events_processed BIGINT DEFAULT 0,
    processing_node VARCHAR(255),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
