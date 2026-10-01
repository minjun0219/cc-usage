package core

import (
	"encoding/json"
	"strings"
	"time"

	"cc-usage/internal/config"
	"cc-usage/internal/store"
)

// Antigravity(agy) 지원. cc-usage 의 주 대상은 Claude Code 이고, 이 파일은 같은
// 렌더러를 agy 에서도 쓰기 위한 덧붙임이다 — Claude 쪽 판단 로직은 여기를 모른다.
//
// agy 는 statusLine stdin 에 Claude Code 와 같은 꼴의 JSON 을 주고, 한도는
// rate_limits 대신 quota 로 준다(1.2.14 실측):
//
//	"quota": {
//	  "gemini-5h":     {"remaining_fraction": 0.86, "reset_time": "2026-10-01T04:15:38Z"},
//	  "gemini-weekly": {"remaining_fraction": 0.94, "reset_time": "2026-10-07T08:19:44Z"},
//	  "3p-5h":         {...}, "3p-weekly": {...}
//	}
//
// 3p 는 Gemini 가 아닌 모델(Claude·GPT-OSS)의 몫이다. 비공식 필드라 전부 optional 로 본다.

// productAntigravity is the stdin `product` value agy sends.
const productAntigravity = "antigravity"

type agyQuota struct {
	RemainingFraction *float64        `json:"remaining_fraction"`
	ResetTime         json.RawMessage `json:"reset_time"`
}

// IsAgy reports whether this payload came from Antigravity rather than Claude Code.
func (in *Input) IsAgy() bool {
	return in.Product == productAntigravity
}

// AgyLimits maps agy's quota onto the 5h/7d windows, picking the bucket that
// the current model draws from.
//
// 버킷은 모델 이름으로 고른다 — 이름에 gemini 가 있으면 gemini-*, 아니면 3p-*.
// payload 에 모델 → 버킷 대응이 따로 오지 않는다. 모델 이름을 모르거나 고른
// 버킷이 없으면 창을 비운다 — 다른 버킷의 숫자를 대신 그리면 지금 모델과 무관한
// 한도가 그럴듯하게 보인다.
//
// FromStdin 을 세운다: 숫자는 stdin 에서 왔고, 렌더가 usage API 상태 문구를 낼
// 이유가 없다.
func AgyLimits(in *Input, now time.Time) Limits {
	name := strings.ToLower(in.Model.DisplayName)
	if name == "" {
		return Limits{FromStdin: true}
	}
	bucket := "3p"
	if strings.Contains(name, "gemini") {
		bucket = "gemini"
	}
	return Limits{
		FiveHour:  dropExpired(agyWindow(in.Quota[bucket+"-5h"]), now),
		SevenDay:  dropExpired(agyWindow(in.Quota[bucket+"-weekly"]), now),
		FromStdin: true,
	}
}

// agyWindow converts a remaining fraction (0-1) into a used percentage.
func agyWindow(q *agyQuota) *store.Window {
	if q == nil || q.RemainingFraction == nil {
		return nil
	}
	r := *q.RemainingFraction
	// 경계의 부동소수 오차(1.0000001, -1e-9)는 잘라 쓴다 — 하필 소진 직후에 창이
	// 사라지면 안 된다. 그보다 크게 벗어나면 단위를 모르는 것이고, 틀린 숫자보다
	// 없는 편이 낫다(Claude 쪽 convert 의 epoch 누출 가드와 같은 생각).
	const eps = 0.01
	if r < -eps || r > 1+eps {
		return nil
	}
	r = min(max(r, 0), 1)
	return &store.Window{Percent: (1 - r) * 100, ResetsAt: parseResets(q.ResetTime)}
}

// LocalLimits decides whether this render stays off Claude's limit path, and
// what to draw there instead.
//
// local 이 true 면 호출자는 token·API·cache·계정 파일을 건드리지 않는다. none 이면
// 한도를 하나도 그리지 않고, agy 면(source 가 none 이 아닌 한) stdin 의 quota 를
// 그린다. agy 는 source 설정과 무관하게 local 이다 — Claude 계정과 상관없는
// 호스트이고, 설정 파일 하나를 두 호스트가 같이 읽어도 되게 하려는 것이다.
func LocalLimits(p *config.Config, in *Input, now time.Time) (lim Limits, alert Alert, local bool) {
	none, agy := p.Source == config.SourceNone, in.IsAgy()
	if !none && !agy {
		return Limits{}, Alert{}, false
	}
	if agy && !none {
		lim = AgyLimits(in, now)
		alert = SteadyAlert(p, lim)
	}
	return lim, alert, true
}

// SteadyAlert is Alerts without the burst — 깜빡임은 단계가 올라간 시각을
// state.json 에 적어야 셀 수 있는데, agy 경로는 cache 를 쓰지 않는다(Claude
// 세션과 같은 파일을 나눠 쓰면 서로의 경보 시각을 덮는다). 그래서 경보는
// 배지로 고정해 낸다.
func SteadyAlert(p *config.Config, lim Limits) Alert {
	level, name, pct, _ := worstWindow(p, lim)
	if level == AlertNone {
		return Alert{}
	}
	return Alert{Level: level, Window: name, Percent: pct}
}
