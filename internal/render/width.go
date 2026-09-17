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
		r >= 0x1F300 && r <= 0x1F64F, // 이모지 (💳 포함)
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x1FA70 && r <= 0x1FAFF:
		return 2
	}
	return 1
}
