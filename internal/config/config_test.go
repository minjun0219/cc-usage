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

func TestAlertPercent(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	// 설정에 없으면 90, 0이면 끔. 0이 "끔"이 되려면 없음과 구분돼야 한다 —
	// 예전에는 0이 기본값 90으로 덮여서 README 가 거짓말을 하고 있었다.
	cases := []struct {
		name string
		in   *float64
		want float64
	}{
		{"설정 없음 → 기본 90", nil, 90},
		{"0 → 임박 경고 끔", f(0), 0},
		{"80 → 그대로", f(80), 80},
		{"범위 밖(-1) → 끔", f(-1), 0},
		{"범위 밖(101) → 끔", f(101), 0},
	}
	for _, c := range cases {
		cfg := &Config{AlertPercent: c.in}
		cfg.ApplyDefaults()
		if got := cfg.Alert(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
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
