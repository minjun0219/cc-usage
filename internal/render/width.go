package render

import "unicode/utf8"

// displayWidth is how many terminal columns s occupies, ignoring ANSI escapes.
//
// 표준 라이브러리에 wcwidth 가 없고 외부 의존성은 쓰지 않으므로, East Asian
// Wide/Fullwidth 구간만 2칸으로 세는 근사다. statusline 에 실제로 들어오는 것은
// ASCII · 한글 · 이모지 몇 개뿐이라 이 정도면 맞는다. Ambiguous 폭(→ 같은 화살표)은
// 터미널마다 갈리는데 대부분 1칸으로 그리므로 1로 둔다.
func displayWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		// ESC [ … <final byte> — 우리가 내는 것은 SGR(색)뿐이라 이 형태면 충분하다.
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				for j++; j < len(s) && (s[j] < '@' || s[j] > '~'); j++ {
				}
				if j < len(s) {
					j++
				}
				i = j
				continue
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w += runeWidth(r)
		i += size
	}
	return w
}

func runeWidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	// 2600-27BF(✅ ⚡ ☀)는 대부분 Ambiguous 라 터미널마다 갈린다. 다만
	// Emoji_Presentation 인 몇 개는 어디서나 2칸이다 — 그것만 집는다.
	switch r {
	case 0x231A, 0x231B, 0x23E9, 0x23EA, 0x23EB, 0x23EC, 0x23F0, 0x23F3,
		0x25FD, 0x25FE, 0x2614, 0x2615, 0x2648, 0x2649, 0x264A, 0x264B,
		0x264C, 0x264D, 0x264E, 0x264F, 0x2650, 0x2651, 0x2652, 0x2653,
		0x267F, 0x2693, 0x26A1, 0x26AA, 0x26AB, 0x26BD, 0x26BE, 0x26C4,
		0x26C5, 0x26CE, 0x26D4, 0x26EA, 0x26F2, 0x26F3, 0x26F5, 0x26FA,
		0x26FD, 0x2705, 0x270A, 0x270B, 0x2728, 0x274C, 0x274E, 0x2753,
		0x2754, 0x2755, 0x2757, 0x2795, 0x2796, 0x2797, 0x27B0, 0x27BF:
		return 2
	}
	switch {
	case r <= 0x115F, // 한글 자모
		r >= 0x2E80 && r <= 0x303E, // CJK 부수 · 기호
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3, // 한글 음절
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60, // 전각
		r >= 0xFFE0 && r <= 0xFFE6,
		// 이모지. 배지가 임의의 사용자 이모지를 이 계산에 태우므로, 흔한 구간을
		// 빠뜨리면 폭이 1칸씩 틀려 크레딧 배치 판단이 경계에서 어긋난다.
		r >= 0x1F300 && r <= 0x1F64F, // 기호·그림 · 감정 (💳 🏢 포함)
		r >= 0x1F680 && r <= 0x1F6FF, // 교통·지도 (🚀 🚗)
		r >= 0x1F7E0 && r <= 0x1F7EB, // 색 원·사각 (🟠 🟦)
		r >= 0x1F900 && r <= 0x1F9FF, // 보충 기호 (🧠 🩵)
		r >= 0x1FA70 && r <= 0x1FAFF: // 확장 A (🪄 🫠)
		return 2
	}
	return 1
}
