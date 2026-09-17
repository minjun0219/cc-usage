package config

import "testing"

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
