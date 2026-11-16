package store

import "testing"

func TestAppSettingsDefaults(t *testing.T) {
	s := open(t)
	got, err := s.AppSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := AppSettings{ParkingID: "", ReleaseTime: "11:00", PaymentAmount: 15, AheadDays: 28}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestAppSettingsRoundTrip(t *testing.T) {
	s := open(t)
	want := AppSettings{ParkingID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ReleaseTime: "10:30", PaymentAmount: 12.5, AheadDays: 14}
	if err := s.SaveAppSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.AppSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

// AheadDays 0 (unset) or out of range keeps the stored value; a stored value
// out of range reads back as the default.
func TestAppSettingsAheadDaysBounds(t *testing.T) {
	s := open(t)
	if err := s.SaveAppSettings(AppSettings{ReleaseTime: "11:00", PaymentAmount: 15, AheadDays: 7}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, -1, 91} {
		if err := s.SaveAppSettings(AppSettings{ReleaseTime: "11:00", PaymentAmount: 15, AheadDays: n}); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.AppSettings(); got.AheadDays != 7 {
			t.Fatalf("AheadDays %d overwrote the stored value: %d", n, got.AheadDays)
		}
	}
	if err := s.SetSetting(keyAheadDays, "500"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AppSettings(); got.AheadDays != DefaultAheadDays {
		t.Fatal(got.AheadDays)
	}
}
