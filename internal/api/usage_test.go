package api

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	body := []byte(`{
	  "five_hour": {"utilization": 42.0, "resets_at": "2026-09-16T12:00:00.123+00:00"},
	  "seven_day": {"utilization": 100, "resets_at": "2026-09-20T00:00:00Z"},
	  "seven_day_opus": null,
	  "extra_usage": {"is_enabled": true, "monthly_limit": 5000, "used_credits": 1234, "utilization": 24.68}
	}`)
	u, err := Parse(body, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if u.FiveHour == nil || u.FiveHour.Percent != 42 || u.FiveHour.ResetsAt.IsZero() {
		t.Fatalf("five_hour: %+v", u.FiveHour)
	}
	if u.SevenDay == nil || u.SevenDay.Percent != 100 {
		t.Fatalf("seven_day: %+v", u.SevenDay)
	}
	if u.Extra == nil || !u.Extra.Enabled || *u.Extra.UsedCredits != 1234 || *u.Extra.MonthlyLimit != 5000 {
		t.Fatalf("extra: %+v", u.Extra)
	}
}

func TestParseMissingFields(t *testing.T) {
	u, err := Parse([]byte(`{"five_hour": null}`), time.Now())
	if err != nil || u.FiveHour != nil || u.Extra != nil {
		t.Fatalf("got %+v, %v", u, err)
	}
}
