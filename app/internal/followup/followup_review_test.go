package followup

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

func TestWrapPicksTaskHoldingThePayment(t *testing.T) {
	// a failed future task shares login+day with the monitor's unconfirmed one
	fut := model.Task{Login: "u", Day: "2026-10-26", Kind: model.KindFuture, Status: model.StatusFailed}
	past := unconfirmedTask(time.Now())
	past.Kind = model.KindPast
	api, st, n := &fakeAPI{confAt: 1}, newStore(fut, past), &notif{}
	f := mk(api, st, n)
	f.Wrap(n).Result(context.Background(), "u", "2026-10-26", unc)
	f.Wait()
	tk, _ := st.GetTask("u", "2026-10-26", model.KindPast)
	if api.calls != 1 || tk.Status != model.StatusPaid {
		t.Fatalf("calls=%d %+v", api.calls, tk)
	}
}

type cancelAPI struct{ cancel context.CancelFunc }

func (c cancelAPI) PaymentStatus(ctx context.Context, jwt, id string) (int, error) {
	c.cancel()
	return 0, ctx.Err()
}

func TestShutdownMidCheckRecordsNothing(t *testing.T) {
	st, n := newStore(unconfirmedTask(time.Now())), &notif{}
	ctx, cancel := context.WithCancel(context.Background())
	f := New(cancelAPI{cancel}, fakeSess{}, st, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Delays = []time.Duration{time.Millisecond}
	f.Run(ctx)
	f.Wait()
	tk, _ := st.GetTask("u", "2026-10-26", model.KindFuture)
	if len(n.list()) != 0 || tk.Detail != "" {
		t.Fatalf("recorded on shutdown: %+v %+v", n.list(), tk)
	}
}
