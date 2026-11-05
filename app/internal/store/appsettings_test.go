package store

import "testing"

func TestAppSettingsDefaults(t *testing.T) {
	s := open(t)
	got, err := s.AppSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := AppSettings{ParkingID: "", ReleaseTime: "11:00", PaymentAmount: 15}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestAppSettingsRoundTrip(t *testing.T) {
	s := open(t)
	want := AppSettings{ParkingID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ReleaseTime: "10:30", PaymentAmount: 12.5}
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
