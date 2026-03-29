-- Add max_passengers to rides (driver sets when creating ride)
ALTER TABLE rides ADD COLUMN max_passengers INTEGER NOT NULL DEFAULT 3;

-- Add passenger request fields to sub_rides
ALTER TABLE sub_rides ADD COLUMN price INTEGER;
ALTER TABLE sub_rides ADD COLUMN preferences JSONB DEFAULT '[]';
ALTER TABLE sub_rides ADD COLUMN baggage VARCHAR(10) DEFAULT 'none' CHECK (baggage IN ('none', 'small', 'large'));
ALTER TABLE sub_rides ADD COLUMN comment TEXT DEFAULT '';
ALTER TABLE sub_rides ADD COLUMN picked_up_at TIMESTAMP;
