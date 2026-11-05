// Package api is the client for the reservation service API.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// Client talks to the service API.
type Client struct {
	base       string
	appVersion string
	hc         *http.Client
	warsaw     *time.Location
}

// New builds a Client. base is e.g. https://app.example.com/api.
func New(base, appVersion string, hc *http.Client) *Client {
	if hc == nil {
		hc = NewHTTPClient()
	}
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.UTC
	}
	return &Client{base: strings.TrimRight(base, "/"), appVersion: appVersion, hc: hc, warsaw: loc}
}

// NewHTTPClient returns a keep-alive HTTP/2 client with no global timeout (use ctx).
func NewHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: tr}
}

type envelope struct {
	IsSuccess bool            `json:"isSuccess"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, jwt string, body, out any) error {
	u := c.base + "/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "pl")
	req.Header.Set("x-pc-mobile-app-version", c.appVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	var env envelope
	jerr := json.Unmarshal(raw, &env)
	if resp.StatusCode/100 != 2 || (jerr == nil && !env.IsSuccess) {
		ae := &model.APIError{HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("http %d", resp.StatusCode)}
		if jerr == nil && env.Error != nil {
			ae.Code, ae.Message = env.Error.Code, env.Error.Message
		}
		return ae
	}
	if jerr != nil {
		return fmt.Errorf("api: bad response: %w", jerr)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("api: bad result: %w", err)
	}
	return nil
}

func jwtExp(jwt string) (time.Time, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return time.Time{}, errors.New("api: malformed jwt")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, fmt.Errorf("api: jwt payload: %w", err)
	}
	var p struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return time.Time{}, fmt.Errorf("api: jwt payload: %w", err)
	}
	return time.Unix(int64(p.Exp), 0), nil
}

// Login creates a session.
func (c *Client) Login(ctx context.Context, login, password string) (model.Session, error) {
	var r struct {
		JWT string `json:"jwt"`
	}
	err := c.do(ctx, "POST", "Sessions/native", nil, "", map[string]string{"login": login, "password": password}, &r)
	if err != nil {
		return model.Session{}, err
	}
	exp, err := jwtExp(r.JWT)
	if err != nil {
		return model.Session{}, err
	}
	return model.Session{JWT: r.JWT, Exp: exp}, nil
}

// Vehicles lists the user's vehicles.
func (c *Client) Vehicles(ctx context.Context, jwt string) ([]model.Vehicle, error) {
	var r []struct {
		ID    string `json:"id"`
		Plate string `json:"licensePlateNumber"`
	}
	if err := c.do(ctx, "GET", "Vehicles", nil, jwt, nil, &r); err != nil {
		return nil, err
	}
	out := make([]model.Vehicle, len(r))
	for i, v := range r {
		out[i] = model.Vehicle{ID: v.ID, Plate: v.Plate}
	}
	return out, nil
}

// CreditCards lists saved cards.
func (c *Client) CreditCards(ctx context.Context, jwt string) ([]model.Card, error) {
	var r []struct {
		ID         string `json:"id"`
		Status     int    `json:"status"`
		ClientHash string `json:"clientHash"`
	}
	if err := c.do(ctx, "GET", "Payments/credit-cards", nil, jwt, nil, &r); err != nil {
		return nil, err
	}
	out := make([]model.Card, len(r))
	for i, v := range r {
		out[i] = model.Card{ID: v.ID, Status: v.Status, ClientHash: v.ClientHash}
	}
	return out, nil
}

// Offers lists spots offered within [start, end).
func (c *Client) Offers(ctx context.Context, jwt, parkingID string, start, end time.Time) (model.Offers, error) {
	q := url.Values{
		"ParkingId": {parkingID},
		"Start":     {start.UTC().Format(timeLayout)},
		"End":       {end.UTC().Format(timeLayout)},
		"Page":      {"1"},
		"PageSize":  {"1000"},
	}
	var r struct {
		TurnstileURL string `json:"turnstileUrl"`
		Entries      []struct {
			ID          string `json:"id"`
			Level       string `json:"level"`
			Handicapped bool   `json:"isForHandicapped"`
			Charger     bool   `json:"electricCharger"`
			MiniEV      bool   `json:"isMiniEvOnly"`
		} `json:"entries"`
	}
	if err := c.do(ctx, "GET", "ParkingOffers/parking-offers-within-time-range-for-specyfic-parking", q, jwt, nil, &r); err != nil {
		return model.Offers{}, err
	}
	o := model.Offers{TurnstileURL: r.TurnstileURL, Spots: make([]model.Spot, len(r.Entries))}
	for i, e := range r.Entries {
		o.Spots[i] = model.Spot{ID: e.ID, Level: e.Level, Handicapped: e.Handicapped, Charger: e.Charger, MiniEV: e.MiniEV}
	}
	return o, nil
}

// Timeline returns free spot counts per Warsaw day.
func (c *Client) Timeline(ctx context.Context, jwt, parkingID string, start time.Time) ([]model.DayFree, error) {
	q := url.Values{"ParkingId": {parkingID}, "Start": {start.UTC().Format(timeLayout)}}
	var r struct {
		Entries []struct {
			Day  string `json:"day"`
			Free int    `json:"freePlacesCount"`
		} `json:"entries"`
	}
	if err := c.do(ctx, "GET", "ParkingOffers/full-day-parking-offers-overview-timeline-for-specyfic-parking", q, jwt, nil, &r); err != nil {
		return nil, err
	}
	out := make([]model.DayFree, len(r.Entries))
	for i, e := range r.Entries {
		t, err := time.Parse(time.RFC3339, e.Day)
		if err != nil {
			return nil, fmt.Errorf("api: bad day %q: %w", e.Day, err)
		}
		out[i] = model.DayFree{Day: t.In(c.warsaw).Format("2006-01-02"), Free: e.Free}
	}
	return out, nil
}

// Reserve books a spot and returns the reservation id.
func (c *Client) Reserve(ctx context.Context, jwt, spotID string, start, end time.Time, token, vehicleID string) (string, error) {
	body := map[string]string{
		"start":          start.UTC().Format(timeLayout),
		"end":            end.UTC().Format(timeLayout),
		"turnstileToken": token,
		"vehicleId":      vehicleID,
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, "POST", "ParkingSpots/"+url.PathEscape(spotID)+"/reservations", nil, jwt, body, &r); err != nil {
		return "", err
	}
	return r.ID, nil
}

// PaymentAmount returns the total price of a reservation.
func (c *Client) PaymentAmount(ctx context.Context, jwt, reservationID string) (float64, error) {
	var r struct {
		Data struct {
			TotalPrice float64 `json:"totalPrice"`
		} `json:"data"`
	}
	if err := c.do(ctx, "GET", "Payments/amount/"+url.PathEscape(reservationID), nil, jwt, nil, &r); err != nil {
		return 0, err
	}
	return r.Data.TotalPrice, nil
}

// ParkingsList lists the parkings available to the user.
func (c *Client) ParkingsList(ctx context.Context, jwt string) ([]model.Parking, error) {
	q := url.Values{"Page": {"1"}, "PageSize": {"1000"}}
	var r struct {
		Entries []struct {
			ID      string `json:"parkingId"`
			Name    string `json:"parkingName"`
			Address struct {
				City         string `json:"city"`
				StreetName   string `json:"streetName"`
				StreetNumber string `json:"streetNumber"`
			} `json:"address"`
			MinPrice float64 `json:"minPrice"`
			MaxPrice float64 `json:"maxPrice"`
		} `json:"entries"`
	}
	if err := c.do(ctx, "GET", "ParkingOffers/user-parkings-with-parking-offers-overview", q, jwt, nil, &r); err != nil {
		return nil, err
	}
	out := make([]model.Parking, 0, len(r.Entries))
	for _, e := range r.Entries {
		if e.ID == "" {
			continue
		}
		out = append(out, model.Parking{
			ID: e.ID, Name: e.Name, City: e.Address.City,
			Street:   strings.TrimSpace(e.Address.StreetName + " " + e.Address.StreetNumber),
			MinPrice: e.MinPrice, MaxPrice: e.MaxPrice,
		})
	}
	return out, nil
}

// ParkingLevels returns the distinct, sorted levels of a parking's spots. It looks at
// the released day with the most free spots (per Timeline from now) and reads that
// day's offers; with no free spot anywhere it returns an empty list.
func (c *Client) ParkingLevels(ctx context.Context, jwt, parkingID string, now time.Time) ([]string, error) {
	days, err := c.Timeline(ctx, jwt, parkingID, now)
	if err != nil {
		return nil, err
	}
	best, bestFree := "", 0
	for _, d := range days {
		if d.Free > bestFree {
			best, bestFree = d.Day, d.Free
		}
	}
	if best == "" {
		return []string{}, nil
	}
	start, end, err := timeutil.DayWindow(best)
	if err != nil {
		return nil, fmt.Errorf("api: bad day %q: %w", best, err)
	}
	offers, err := c.Offers(ctx, jwt, parkingID, start, end)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, s := range offers.Spots {
		l := strings.TrimSpace(s.Level)
		if l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out, nil
}

// StartCardPayment pays a reservation with a saved card and returns the payment id.
func (c *Client) StartCardPayment(ctx context.Context, jwt, cardID string, amount float64, reservationID, ip string) (string, error) {
	body := map[string]any{
		"cardId":            cardID,
		"amount":            amount,
		"paymentTargetType": 0,
		"targetObjectId":    reservationID,
		"ipAddress":         ip,
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, "POST", "Payments/start-saved-credit-card-payment", nil, jwt, body, &r); err != nil {
		return "", err
	}
	return r.ID, nil
}

// PaymentStatus returns the payment status (2 = confirmed).
func (c *Client) PaymentStatus(ctx context.Context, jwt, paymentID string) (int, error) {
	var r struct {
		Status int `json:"status"`
	}
	if err := c.do(ctx, "GET", "Payments/payment-status", url.Values{"PaymentId": {paymentID}}, jwt, nil, &r); err != nil {
		return 0, err
	}
	return r.Status, nil
}
