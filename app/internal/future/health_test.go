package future

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunDayReportsHealth(t *testing.T) {
	var gotErr error
	var gotDetail string
	calls := 0
	f := newF(&fakeStore{users: users("a", "bad")}, fakePrep{fail: "bad"}, &fakeRun{}, &fakeNotify{})
	f.Health = func(err error, detail string) { calls++; gotErr, gotDetail = err, detail }
	if err := f.RunDay(context.Background(), time.Now().Add(50*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotErr == nil || !strings.Contains(gotDetail, "opłacono 1") || !strings.Contains(gotDetail, "nieudane 1") {
		t.Fatalf("health %d %v %q", calls, gotErr, gotDetail)
	}

	f = newF(&fakeStore{users: users("a")}, fakePrep{}, &fakeRun{}, &fakeNotify{})
	f.Health = func(err error, detail string) { gotErr, gotDetail = err, detail }
	if err := f.RunDay(context.Background(), time.Now().Add(50*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if gotErr != nil || !strings.Contains(gotDetail, "nieudane 0") {
		t.Fatalf("clean run %v %q", gotErr, gotDetail)
	}
}
