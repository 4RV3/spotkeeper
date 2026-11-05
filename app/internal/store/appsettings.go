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
}

const (
	keyParkingID     = "parking_id"
	keyReleaseTime   = "release_time"
	keyPaymentAmount = "payment_amount"
)

// AppSettings returns the stored settings; missing keys fall back to defaults.
func (s *Store) AppSettings() (AppSettings, error) {
	out := AppSettings{ReleaseTime: "11:00", PaymentAmount: 15}
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
	return out, nil
}

// SaveAppSettings stores all three settings.
func (s *Store) SaveAppSettings(a AppSettings) error {
	if err := s.SetSetting(keyParkingID, a.ParkingID); err != nil {
		return err
	}
	if err := s.SetSetting(keyReleaseTime, a.ReleaseTime); err != nil {
		return err
	}
	return s.SetSetting(keyPaymentAmount, strconv.FormatFloat(a.PaymentAmount, 'f', -1, 64))
}
