ALTER TABLE rides ADD COLUMN expires_at TIMESTAMP;

-- Set expiry for existing pending rides (3 hours from creation)
UPDATE rides SET expires_at = created_at + INTERVAL '3 hours' WHERE status = 'pending' AND expires_at IS NULL;
