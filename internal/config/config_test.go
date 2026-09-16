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

func TestStatusLabel(t *testing.T) {
	empty, named := "", "work"
	cases := []struct {
		p              *Profile
		label, display string
	}{
		{&Profile{Name: "personal"}, "personal", "personal"},        // 설정 없음 → profile 이름
		{&Profile{Name: "personal", Label: &empty}, "", "personal"}, // 빈 값 → 세그먼트 생략
		{&Profile{Name: "w", Label: &named}, "work", "work"},
	}
	for _, c := range cases {
		if got := c.p.StatusLabel(); got != c.label {
			t.Errorf("StatusLabel: got %q want %q", got, c.label)
		}
		if got := c.p.Display(); got != c.display {
			t.Errorf("Display는 비면 안 된다: got %q want %q", got, c.display)
		}
	}
}

func TestAlertPercentDefaults(t *testing.T) {
	for in, want := range map[float64]float64{0: 90, 80: 80, -1: 0, 101: 0} {
		p := &Profile{Name: "p", AlertPercent: in}
		p.ApplyDefaults()
		if p.AlertPercent != want {
			t.Errorf("alert_percent %v → %v, want %v", in, p.AlertPercent, want)
		}
	}
}

func TestNotifyStateHidesArgs(t *testing.T) {
	secret := "https://hooks.example.com/T000/B000/XXXXsecretXXXX"
	p := &Profile{Name: "p", Notify: &Notify{Command: []string{"curl", "-d", "msg", secret}}}
	got := p.NotifyState()
	if strings.Contains(got, secret) {
		t.Errorf("webhook URL이 doctor 출력에 남으면 안 된다: %s", got)
	}
	if !strings.Contains(got, "curl") || !strings.Contains(got, "3") {
		t.Errorf("실행 파일명과 인자 개수는 보여야 한다: %s", got)
	}
}
