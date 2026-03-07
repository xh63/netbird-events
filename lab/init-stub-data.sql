-- netbird-events lab: stub NetBird tables with realistic test data
-- Run this for simulated mode (no real NetBird instance needed)
-- Safe to re-run (uses IF NOT EXISTS + ON CONFLICT DO NOTHING)

-- Accounts
CREATE TABLE IF NOT EXISTS accounts (
    id VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    domain VARCHAR(255)
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(255) PRIMARY KEY,
    account_id VARCHAR(255),
    email VARCHAR(255),
    name VARCHAR(255),
    role VARCHAR(50) DEFAULT 'user',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Peers (devices)
CREATE TABLE IF NOT EXISTS peers (
    id VARCHAR(255) PRIMARY KEY,
    account_id VARCHAR(255),
    name VARCHAR(255),
    ip VARCHAR(50),
    os VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Events (NetBird audit log)
CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    activity INTEGER NOT NULL,
    initiator_id VARCHAR(255),
    target_id VARCHAR(255),
    account_id VARCHAR(255),
    meta JSONB
);

-- ============================================================================
-- Seed data
-- ============================================================================

INSERT INTO accounts (id, domain) VALUES
    ('account-001', 'example.com')
ON CONFLICT DO NOTHING;

INSERT INTO users (id, account_id, email, name, role) VALUES
    ('user-alice', 'account-001', 'alice@example.com', 'Alice',   'admin'),
    ('user-bob',   'account-001', 'bob@example.com',   'Bob',     'user'),
    ('user-carol', 'account-001', 'carol@example.com', 'Carol',   'user'),
    ('user-dan',   'account-001', 'dan@example.com',   'Dan',     'user')
ON CONFLICT DO NOTHING;

INSERT INTO peers (id, account_id, name, ip, os) VALUES
    ('peer-laptop-alice', 'account-001', 'alice-macbook',  '100.64.0.1', 'macOS 14'),
    ('peer-laptop-bob',   'account-001', 'bob-ubuntu',     '100.64.0.2', 'Ubuntu 22.04'),
    ('peer-phone-carol',  'account-001', 'carol-iphone',   '100.64.0.3', 'iOS 17'),
    ('peer-server-01',    'account-001', 'prod-server-01', '100.64.0.4', 'Rocky Linux 9')
ON CONFLICT DO NOTHING;

-- Activity codes from NetBird:
--   1  = user.signin              7  = peer.add
--   2  = user.signup              8  = peer.delete
--   3  = user.delete             10  = group.create
--   4  = user.update             11  = group.delete
--   5  = user.role.update        14  = group.peer.add
--  52  = integration.create      30  = setupkey.create
--  53  = integration.delete      31  = setupkey.revoke

INSERT INTO events (timestamp, activity, initiator_id, target_id, account_id, meta) VALUES
    (NOW() - INTERVAL '23 hours', 1,  'user-alice', 'user-alice',       'account-001', '{"ip":"203.0.113.10"}'),
    (NOW() - INTERVAL '22 hours', 30, 'user-alice', 'setupkey-001',     'account-001', '{"name":"onboarding","expires":"2026-06-01"}'),
    (NOW() - INTERVAL '21 hours', 7,  'user-bob',   'peer-laptop-bob',  'account-001', '{"ip":"100.64.0.2","os":"Ubuntu 22.04"}'),
    (NOW() - INTERVAL '20 hours', 7,  'user-alice', 'peer-laptop-alice','account-001', '{"ip":"100.64.0.1","os":"macOS 14"}'),
    (NOW() - INTERVAL '18 hours', 10, 'user-alice', 'group-eng',        'account-001', '{"name":"Engineering"}'),
    (NOW() - INTERVAL '17 hours', 14, 'user-alice', 'peer-laptop-bob',  'account-001', '{"group":"Engineering"}'),
    (NOW() - INTERVAL '16 hours', 14, 'user-alice', 'peer-laptop-alice','account-001', '{"group":"Engineering"}'),
    (NOW() - INTERVAL '12 hours', 7,  'user-carol', 'peer-phone-carol', 'account-001', '{"ip":"100.64.0.3","os":"iOS 17"}'),
    (NOW() - INTERVAL '10 hours', 5,  'user-alice', 'user-bob',         'account-001', '{"old_role":"user","new_role":"admin"}'),
    (NOW() - INTERVAL '8 hours',  1,  'user-bob',   'user-bob',         'account-001', '{"ip":"198.51.100.5"}'),
    (NOW() - INTERVAL '6 hours',  7,  'user-alice', 'peer-server-01',   'account-001', '{"ip":"100.64.0.4","os":"Rocky Linux 9"}'),
    (NOW() - INTERVAL '4 hours',  14, 'user-alice', 'peer-server-01',   'account-001', '{"group":"Engineering"}'),
    (NOW() - INTERVAL '2 hours',  52, 'user-alice', 'integration-slack','account-001', '{"provider":"slack","name":"Slack Alerts"}'),
    (NOW() - INTERVAL '1 hour',   4,  'user-alice', 'user-carol',       'account-001', '{"field":"name","old":"Carol C","new":"Carol"}'),
    (NOW() - INTERVAL '30 mins',  1,  'user-carol', 'user-carol',       'account-001', '{"ip":"203.0.113.99"}');
