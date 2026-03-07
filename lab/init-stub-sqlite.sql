-- netbird-events lab: stub NetBird tables for simulated SQLite mode
-- Creates events, users, accounts tables and seeds realistic test data
-- Safe to re-run (uses IF NOT EXISTS + INSERT OR IGNORE)

CREATE TABLE IF NOT EXISTS accounts (
    id TEXT PRIMARY KEY,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    domain TEXT
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    account_id TEXT,
    email TEXT,
    name TEXT,
    role TEXT DEFAULT 'user',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS peers (
    id TEXT PRIMARY KEY,
    account_id TEXT,
    name TEXT,
    ip TEXT,
    os TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Activity codes from NetBird:
--   1  = user.signin              7  = peer.add
--   2  = user.signup              8  = peer.delete
--   3  = user.delete             10  = group.create
--   4  = user.update             11  = group.delete
--   5  = user.role.update        14  = group.peer.add
--  52  = integration.create      30  = setupkey.create
--  53  = integration.delete      31  = setupkey.revoke
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    activity INTEGER NOT NULL,
    initiator_id TEXT,
    target_id TEXT,
    account_id TEXT,
    meta TEXT
);

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

-- ============================================================================
-- Seed data
-- ============================================================================

INSERT OR IGNORE INTO accounts (id, domain) VALUES
    ('account-001', 'example.com');

INSERT OR IGNORE INTO users (id, account_id, email, name, role) VALUES
    ('user-alice', 'account-001', 'alice@example.com', 'Alice', 'admin'),
    ('user-bob',   'account-001', 'bob@example.com',   'Bob',   'user'),
    ('user-carol', 'account-001', 'carol@example.com', 'Carol', 'user'),
    ('user-dan',   'account-001', 'dan@example.com',   'Dan',   'user');

INSERT OR IGNORE INTO peers (id, account_id, name, ip, os) VALUES
    ('peer-laptop-alice', 'account-001', 'alice-macbook',  '100.64.0.1', 'macOS 14'),
    ('peer-laptop-bob',   'account-001', 'bob-ubuntu',     '100.64.0.2', 'Ubuntu 22.04'),
    ('peer-phone-carol',  'account-001', 'carol-iphone',   '100.64.0.3', 'iOS 17'),
    ('peer-server-01',    'account-001', 'prod-server-01', '100.64.0.4', 'Rocky Linux 9');

INSERT OR IGNORE INTO events (timestamp, activity, initiator_id, target_id, account_id, meta) VALUES
    (datetime('now', '-23 hours'), 1,  'user-alice', 'user-alice',        'account-001', '{"ip":"203.0.113.10"}'),
    (datetime('now', '-22 hours'), 30, 'user-alice', 'setupkey-001',      'account-001', '{"name":"onboarding","expires":"2026-06-01"}'),
    (datetime('now', '-21 hours'), 7,  'user-bob',   'peer-laptop-bob',   'account-001', '{"ip":"100.64.0.2","os":"Ubuntu 22.04"}'),
    (datetime('now', '-20 hours'), 7,  'user-alice', 'peer-laptop-alice', 'account-001', '{"ip":"100.64.0.1","os":"macOS 14"}'),
    (datetime('now', '-18 hours'), 10, 'user-alice', 'group-eng',         'account-001', '{"name":"Engineering"}'),
    (datetime('now', '-17 hours'), 14, 'user-alice', 'peer-laptop-bob',   'account-001', '{"group":"Engineering"}'),
    (datetime('now', '-16 hours'), 14, 'user-alice', 'peer-laptop-alice', 'account-001', '{"group":"Engineering"}'),
    (datetime('now', '-12 hours'), 7,  'user-carol', 'peer-phone-carol',  'account-001', '{"ip":"100.64.0.3","os":"iOS 17"}'),
    (datetime('now', '-10 hours'), 5,  'user-alice', 'user-bob',          'account-001', '{"old_role":"user","new_role":"admin"}'),
    (datetime('now', '-8 hours'),  1,  'user-bob',   'user-bob',          'account-001', '{"ip":"198.51.100.5"}'),
    (datetime('now', '-6 hours'),  7,  'user-alice', 'peer-server-01',    'account-001', '{"ip":"100.64.0.4","os":"Rocky Linux 9"}'),
    (datetime('now', '-4 hours'),  14, 'user-alice', 'peer-server-01',    'account-001', '{"group":"Engineering"}'),
    (datetime('now', '-2 hours'),  52, 'user-alice', 'integration-slack', 'account-001', '{"provider":"slack","name":"Slack Alerts"}'),
    (datetime('now', '-1 hours'),  4,  'user-alice', 'user-carol',        'account-001', '{"field":"name","old":"Carol C","new":"Carol"}'),
    (datetime('now', '-30 minutes'), 1, 'user-carol', 'user-carol',       'account-001', '{"ip":"203.0.113.99"}');
