-- Admin settings (single-row table)
CREATE TABLE admin_settings (
    id              INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    settings        JSONB NOT NULL DEFAULT '{}',
    matching_params JSONB NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- New columns on rides
ALTER TABLE rides ADD COLUMN price          INTEGER;
ALTER TABLE rides ADD COLUMN distance_m     INTEGER;
ALTER TABLE rides ADD COLUMN from_address   VARCHAR(255);
ALTER TABLE rides ADD COLUMN to_address     VARCHAR(255);
ALTER TABLE rides ADD COLUMN payment_method VARCHAR(20);
ALTER TABLE rides ADD COLUMN completed_at   TIMESTAMP;
ALTER TABLE rides ADD COLUMN cancelled_at   TIMESTAMP;

-- New columns on sub_rides
ALTER TABLE sub_rides ADD COLUMN completed_at TIMESTAMP;
ALTER TABLE sub_rides ADD COLUMN cancelled_at TIMESTAMP;

-- Replace idx_rides_status with composite index
DROP INDEX IF EXISTS idx_rides_status;
CREATE INDEX idx_rides_status_created ON rides (status, created_at DESC);
CREATE INDEX idx_sub_rides_ride_id_status ON sub_rides (ride_id, status);
