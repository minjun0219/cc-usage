package config

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestNotifyOn(t *testing.T) {
	cmd := []string{"osascript", "-e", "..."}
	cases := []struct {
		name string
		n    *Notify
		want bool
	}{
		{"설정 없음", nil, false},
		{"command 없음", &Notify{}, false},
		{"enabled 생략은 켜짐", &Notify{Command: cmd}, true},
		{"명시적으로 켬", &Notify{Enabled: boolPtr(true), Command: cmd}, true},
		{"명시적으로 끔 — 명령은 남아 있다", &Notify{Enabled: boolPtr(false), Command: cmd}, false},
	}
	for _, c := range cases {
		if got := c.n.On(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestAlertPercentDefaults(t *testing.T) {
	for in, want := range map[float64]float64{0: 90, 80: 80, -1: 0, 101: 0} {
		p := &Config{AlertPercent: in}
		p.ApplyDefaults()
		if p.AlertPercent != want {
			t.Errorf("alert_percent %v → %v, want %v", in, p.AlertPercent, want)
		}
	}
}

func TestNotifyStateHidesArgs(t *testing.T) {
	secret := "https://hooks.example.com/T000/B000/XXXXsecretXXXX"
	p := &Config{Notify: &Notify{Command: []string{"curl", "-d", "msg", secret}}}
	got := p.NotifyState()
	if strings.Contains(got, secret) {
		t.Errorf("webhook URL이 doctor 출력에 남으면 안 된다: %s", got)
	}
	if !strings.Contains(got, "curl") || !strings.Contains(got, "3") {
		t.Errorf("실행 파일명과 인자 개수는 보여야 한다: %s", got)
	}
}
