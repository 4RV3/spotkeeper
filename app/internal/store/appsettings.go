package store

import (
	"errors"
	"strconv"

	"spotkeeper/internal/model"
)

// AppSettings is the user-editable configuration kept in the settings table.
type AppSettings struct {
	ParkingID     string
	ReleaseTime   string
	PaymentAmount float64
	// AheadDays is how many days ahead of the release day the reservation targets.
	AheadDays int
}

// Default and allowed range of AppSettings.AheadDays.
const (
	DefaultAheadDays = 28
	MinAheadDays     = 1
	MaxAheadDays     = 90
)

const (
	keyParkingID     = "parking_id"
	keyReleaseTime   = "release_time"
	keyPaymentAmount = "payment_amount"
	keyAheadDays     = "ahead_days"
)

// AppSettings returns the stored settings; missing keys fall back to defaults.
func (s *Store) AppSettings() (AppSettings, error) {
	out := AppSettings{ReleaseTime: "11:00", PaymentAmount: 15, AheadDays: DefaultAheadDays}
	v, err := s.GetSetting(keyParkingID)
	if err == nil {
		out.ParkingID = v
	} else if !errors.Is(err, model.ErrNotFound) {
		return out, err
	}
	v, err = s.GetSetting(keyReleaseTime)
	if err == nil {
		out.ReleaseTime = v
	} else if !errors.Is(err, model.ErrNotFound) {
		return out, err
	}
	v, err = s.GetSetting(keyPaymentAmount)
	if err == nil {
		out.PaymentAmount, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return out, err
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return out, err
	}
	v, err = s.GetSetting(keyAheadDays)
	if err == nil {
		n, err := strconv.Atoi(v)
		if err != nil {
			return out, err
		}
		if n >= MinAheadDays && n <= MaxAheadDays {
			out.AheadDays = n
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return out, err
	}
	return out, nil
}

// SaveAppSettings stores all settings.
func (s *Store) SaveAppSettings(a AppSettings) error {
	if err := s.SetSetting(keyParkingID, a.ParkingID); err != nil {
		return err
	}
	if err := s.SetSetting(keyReleaseTime, a.ReleaseTime); err != nil {
		return err
	}
	if a.AheadDays >= MinAheadDays && a.AheadDays <= MaxAheadDays {
		if err := s.SetSetting(keyAheadDays, strconv.Itoa(a.AheadDays)); err != nil {
			return err
		}
	}
	return s.SetSetting(keyPaymentAmount, strconv.FormatFloat(a.PaymentAmount, 'f', -1, 64))
}
