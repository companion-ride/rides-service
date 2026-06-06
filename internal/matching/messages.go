package matching

// Topics
const (
	TopicRideRequests      = "ride-requests"
	TopicRideNotifications = "ride-notifications"
	TopicRideUpdates       = "ride-updates"
	TopicDriverAssigned    = "driver-assigned"
	// TopicRideEvents carries ride lifecycle events consumed by chat-service to
	// create/close the driver↔passenger chat room. Must stay in sync with
	// chat-service's TopicRideEvents.
	TopicRideEvents = "ride-events"
)

// Chat ride-event names (must match chat-service ride_handler.go).
const (
	ChatEventDriverAssigned = "driver_assigned"
	ChatEventRideCompleted  = "ride_completed"
	ChatEventRideCancelled  = "ride_cancelled"
)

// RideEvent is published to TopicRideEvents and consumed by chat-service to
// manage the ride chat room. Field shape must match chat-service's RideEvent.
type RideEvent struct {
	Event       string `json:"event"`
	RideID      string `json:"ride_id"`
	DriverID    string `json:"driver_id,omitempty"`
	PassengerID string `json:"passenger_id,omitempty"`
}

// Actions (inbound from ride-requests topic)
const (
	ActionRequest          = "request"
	ActionOfferResponse    = "offer_response"
	ActionRideStart        = "ride_start"
	ActionPassengerPickup  = "passenger_pickup"
	ActionPassengerDropoff = "passenger_dropoff"
	ActionRideFinish       = "ride_finish"
	ActionSearchCancel     = "search_cancel"
	ActionCancel           = "cancel"
)

// InboundCoord represents coordinates as sent by socketio-handler (latitude/longitude).
type InboundCoord struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// InboundMessage is the envelope for all messages on the ride-requests topic.
type InboundMessage struct {
	Action      string        `json:"action"`
	RequestID   string        `json:"request_id,omitempty"`
	PassengerID string        `json:"passenger_id,omitempty"`
	UserID      string        `json:"user_id,omitempty"`
	RideID      string        `json:"ride_id,omitempty"`
	SubRideID   string        `json:"sub_ride_id,omitempty"`
	Pickup      *InboundCoord `json:"pickup,omitempty"`
	Dropoff     *InboundCoord `json:"dropoff,omitempty"`
	Price       *int          `json:"price,omitempty"`
	Preferences []string      `json:"preferences,omitempty"`
	Baggage     string        `json:"baggage,omitempty"`
	Comment     string        `json:"comment,omitempty"`
	Accepted    *bool         `json:"accepted,omitempty"`
	Reason      string        `json:"reason,omitempty"`
}

// OutboundCoord uses lat/lon (rides service convention).
type OutboundCoord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// NotificationMessage is sent to ride-notifications topic (direct-to-user).
type NotificationMessage struct {
	UserID         string         `json:"user_id"`
	Event          string         `json:"event"`
	RequestID      string         `json:"request_id,omitempty"`
	PassengerID    string         `json:"passenger_id,omitempty"`
	RideID         string         `json:"ride_id,omitempty"`
	SubRideID      string         `json:"sub_ride_id,omitempty"`
	Price          int            `json:"price,omitempty"`
	Preferences    []string       `json:"preferences,omitempty"`
	Baggage        string         `json:"baggage,omitempty"`
	Comment        string         `json:"comment,omitempty"`
	Pickup         *OutboundCoord `json:"pickup,omitempty"`
	Dropoff        *OutboundCoord `json:"dropoff,omitempty"`
	PickupWalkM    float64        `json:"pickup_walk_m,omitempty"`
	DropoffWalkM   float64        `json:"dropoff_walk_m,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Message        string         `json:"message,omitempty"`
}

// RideUpdateMessage is sent to ride-updates topic (broadcast to room).
type RideUpdateMessage struct {
	RideID      string `json:"ride_id"`
	Event       string `json:"event"`
	Status      string `json:"status,omitempty"`
	SubRideID   string `json:"sub_ride_id,omitempty"`
	PassengerID string `json:"passenger_id,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// DriverAssignedMessage is sent to driver-assigned topic.
// RouteID is included so the passenger app can fetch the driver's route
// geometry from pathfinder without waiting for the follow-up driver_route event.
type DriverAssignedMessage struct {
	RideID       string        `json:"ride_id"`
	PassengerID  string        `json:"passenger_id"`
	DriverID     string        `json:"driver_id"`
	SubRideID    string        `json:"sub_ride_id"`
	RouteID      string        `json:"route_id"`
	PickupPoint  OutboundCoord `json:"pickup_point"`
	DropoffPoint OutboundCoord `json:"dropoff_point"`
	PickupWalkM  float64       `json:"pickup_walk_m"`
	DropoffWalkM float64       `json:"dropoff_walk_m"`
}

// Event names for the ride-notifications topic emitted as follow-ups to a
// successful driver assignment. socketio-handler dispatches by user_id and
// emits the named event directly to that user's socket.
const (
	EventDriverRoute       = "driver_route"
	EventPassengerLocation = "passenger_location"
)

// DriverRouteMessage is sent to the passenger right after driver_assigned.
// The passenger app uses route_id to fetch the full route from pathfinder
// via the gateway (GET /api/pathfinder/routes/<route_id>) with its own JWT.
type DriverRouteMessage struct {
	UserID    string `json:"user_id"`
	Event     string `json:"event"`
	RideID    string `json:"ride_id"`
	SubRideID string `json:"sub_ride_id"`
	DriverID  string `json:"driver_id"`
	RouteID   string `json:"route_id"`
}

// PassengerLocationMessage is a one-time snapshot of the passenger's last
// known location, sent to the driver right after driver_assigned. Best-effort:
// the rides service skips this event when the location is unavailable.
type PassengerLocationMessage struct {
	UserID      string  `json:"user_id"`
	Event       string  `json:"event"`
	RideID      string  `json:"ride_id"`
	SubRideID   string  `json:"sub_ride_id"`
	PassengerID string  `json:"passenger_id"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}
