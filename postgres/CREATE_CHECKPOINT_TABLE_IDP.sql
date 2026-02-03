-- Production: Create event_processing_checkpoint table in ais schema
-- Purpose: Track processing progress for eventsproc
-- Run this in production before deploying eventsproc

-- Ensure ais schema exists
CREATE SCHEMA IF NOT EXISTS idp;

-- Create checkpoint table
CREATE TABLE idp.event_processing_checkpoint (
    -- Consumer identifier (e.g., "eventsproc-prod-emea", "eventsproc-prod-apac")
    consumer_id VARCHAR(255) PRIMARY KEY,

    -- Last successfully processed event ID
    last_event_id BIGINT NOT NULL,

    -- Timestamp of the last processed event (for reference/debugging)
    last_event_timestamp TIMESTAMPTZ NOT NULL,

    -- Number of events processed in total
    total_events_processed BIGINT DEFAULT 0,

    -- Processing node hostname (for tracking which instance processed events)
    processing_node VARCHAR(255),

    -- Last update timestamp
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    -- Creation timestamp
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create index on updated_at for monitoring stale consumers
CREATE INDEX idx_checkpoint_updated_at ON idp.event_processing_checkpoint(updated_at);

-- Add table comment
COMMENT ON TABLE idp.event_processing_checkpoint IS
'Tracks processing progress for eventsproc to enable resumption after restart/failover';

-- Add column comments
COMMENT ON COLUMN idp.event_processing_checkpoint.consumer_id IS
'Unique identifier for the consumer instance (format: eventsproc-{platform}-{region})';

COMMENT ON COLUMN idp.event_processing_checkpoint.last_event_id IS
'The ID of the last event successfully processed and sent to output';

COMMENT ON COLUMN idp.event_processing_checkpoint.last_event_timestamp IS
'Timestamp of the last processed event (copied from events.timestamp)';

COMMENT ON COLUMN idp.event_processing_checkpoint.total_events_processed IS
'Running counter of total events processed by this consumer';

COMMENT ON COLUMN idp.event_processing_checkpoint.processing_node IS
'Hostname of the node that processed the events (for HA deployments tracking)';

-- Verify table was created
SELECT
    schemaname,
    tablename,
    tableowner
FROM pg_tables
WHERE schemaname = 'idp'
AND tablename = 'event_processing_checkpoint';

-- Show table structure
\d idp.event_processing_checkpoint
