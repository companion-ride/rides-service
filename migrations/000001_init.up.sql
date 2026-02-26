CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS rides (
    ride_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id UUID NOT NULL,
    route_id UUID NOT NULL,
    geometry geometry(LINESTRING, 4326) NOT NULL,
    start_lat DOUBLE PRECISION NOT NULL,
    start_lon DOUBLE PRECISION NOT NULL,
    finish_lat DOUBLE PRECISION NOT NULL,
    finish_lon DOUBLE PRECISION NOT NULL,
    total_duration INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rides_geometry ON rides USING GIST (geometry);
CREATE INDEX IF NOT EXISTS idx_rides_status ON rides (status);

CREATE TABLE IF NOT EXISTS sub_rides (
    sub_ride_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ride_id UUID NOT NULL REFERENCES rides(ride_id),
    passenger_id UUID NOT NULL,
    pickup_lat DOUBLE PRECISION NOT NULL,
    pickup_lon DOUBLE PRECISION NOT NULL,
    dropoff_lat DOUBLE PRECISION NOT NULL,
    dropoff_lon DOUBLE PRECISION NOT NULL,
    start_lat DOUBLE PRECISION NOT NULL,
    start_lon DOUBLE PRECISION NOT NULL,
    finish_lat DOUBLE PRECISION NOT NULL,
    finish_lon DOUBLE PRECISION NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sub_rides_ride_id ON sub_rides (ride_id);
CREATE INDEX IF NOT EXISTS idx_sub_rides_passenger_id ON sub_rides (passenger_id);
