// Package model holds the shared types used across spotkeeper packages.
package model

import (
	"errors"
	"time"
)

// Session is an authenticated the service session.
type Session struct {
	JWT string
	Exp time.Time
}

// Vehicle is a registered vehicle.
type Vehicle struct {
	ID    string
	Plate string
}

// Card is a payment card.
type Card struct {
	ID         string
	Status     int
	ClientHash string
}

// Spot is a parking spot offered for reservation.
type Spot struct {
	ID          string
	Level       string
	Handicapped bool
	Charger     bool
	MiniEV      bool
}

// Offers is the set of spots currently offered plus the turnstile URL.
type Offers struct {
	Spots        []Spot
	TurnstileURL string
}

// DayFree is the number of free spots on a day.
type DayFree struct {
	Day  string
	Free int
}

// Parking is a parking the user can book, as listed by the service.
type Parking struct {
	ID       string
	Name     string
	City     string
	Street   string // street name and number
	MinPrice float64
	MaxPrice float64
}

// Token is a captured turnstile token.
type Token struct {
	Value      string
	ObservedAt time.Time
}

// APIError is an error response from the the service API.
type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
}

func (e APIError) Error() string {
	return e.Message
}

// TokenError is an error while obtaining a turnstile token.
type TokenError struct {
	Retryable bool
	Code      string
}

func (e TokenError) Error() string {
	return "token error: " + e.Code
}

// User is the single account SpotKeeper serves.
type User struct {
	Login    string
	Plate    string
	Level    string
	IP       string
	Schedule string
	Active   bool
	// SkipHolidays makes the daily run skip Polish public holidays.
	SkipHolidays bool
}

// Task is a scheduled reservation or payment task.
type Task struct {
	Login         string
	Day           string
	Kind          string
	Status        string
	SpotID        string
	ReservationID string
	PaymentID     string
	Detail        string
	Attempts      int
	UpdatedAt     time.Time
}

// Job is the input for one reservation run.
type Job struct {
	Login     string
	JWT       string
	VehicleID string
	CardID    string
	IP        string
	Level     string
	ParkingID string
	Day       string
	Start     time.Time
	End       time.Time
	NotBefore time.Time
}

// Result is the outcome of one reservation run.
type Result struct {
	Status        string
	SpotID        string
	ReservationID string
	PaymentID     string
	Err           string
	Attempts      int
	// AlreadyBooked: the service refuses another booking for this time (code 106 in a
	// row) — the user most likely already has a reservation for the day.
	AlreadyBooked bool
	// Note is an extra sentence for the user's notification.
	Note string
	// Monitor retries: attempt Attempt of MaxTries failed and the next one follows
	// after RetryIn; GaveUp means the monitor stopped after MaxTries failures. Reason
	// is the underlying error (Err keeps the full text for the task history).
	Attempt, MaxTries int
	RetryIn           time.Duration
	GaveUp            bool
	Reason            string
}

// EngineConfig configures the reservation engine.
type EngineConfig struct {
	MaxAttempts   int
	PayPolls      int
	PollInterval  time.Duration
	Budget        time.Duration
	PayInterval   time.Duration
	PaymentAmount float64
	// TurnstilePrefix is the only URL prefix accepted for verification URLs.
	TurnstilePrefix string
}

// Task and result statuses.
const (
	StatusPending     = "pending"
	StatusRunning     = "running"
	StatusPaid        = "paid"
	StatusUnconfirmed = "unconfirmed"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
	// StatusSkipped marks a scheduled day the 11:00 run left out (day off, holiday).
	StatusSkipped = "skipped"
)

// Task kinds.
const (
	KindFuture = "future"
	KindPast   = "past"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")
