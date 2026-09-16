//! Go 핫패스와 **같은 일**을 하는 Rust 구현 — 언어가 건드릴 수 있는 부분만
//! 재기 위한 스파이크다. git 호출과 extra_commands 는 언어와 무관하므로 뺐다.
//!
//! 하는 일 (cmd/cc-usage/main.go 의 runStatusline 에서 git/extra 를 뺀 것):
//!   설정 읽기 → stdin JSON 파싱 → state.json·usage.json 읽기
//!   → 한도 병합 → 경보 판단 → state.json 원자적 쓰기 → 한 줄 렌더
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::io::{Read, Write};
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

// ---------- config ----------

#[derive(Deserialize, Default)]
struct Config {
    #[serde(default)]
    default_profile: String,
    #[serde(default)]
    profiles: HashMap<String, Profile>,
}

#[derive(Deserialize, Default, Clone)]
struct Profile {
    label: Option<String>,
    #[serde(default)]
    source: String,
    #[serde(default)]
    alert_percent: f64,
}

impl Profile {
    fn apply_defaults(&mut self) {
        if self.source.is_empty() {
            self.source = "auto".into();
        }
        if self.alert_percent == 0.0 {
            self.alert_percent = 90.0;
        }
        if !(0.0..=100.0).contains(&self.alert_percent) {
            self.alert_percent = 0.0;
        }
    }
    fn status_label(&self, name: &str) -> String {
        self.label.clone().unwrap_or_else(|| name.to_string())
    }
}

// ---------- stdin ----------

#[derive(Deserialize, Default)]
struct Input {
    #[serde(default)]
    model: Model,
    #[serde(default)]
    context_window: Ctx,
    #[serde(default)]
    rate_limits: Option<RateLimits>,
}
#[derive(Deserialize, Default)]
struct Model {
    #[serde(default)]
    display_name: String,
}
#[derive(Deserialize, Default)]
struct Ctx {
    used_percentage: Option<f64>,
}
#[derive(Deserialize, Default)]
struct RateLimits {
    five_hour: Option<StdinWindow>,
    seven_day: Option<StdinWindow>,
}
#[derive(Deserialize)]
struct StdinWindow {
    used_percentage: Option<f64>,
    #[serde(default)]
    resets_at: Option<serde_json::Value>,
}

impl StdinWindow {
    fn convert(&self) -> Option<Window> {
        let p = self.used_percentage?;
        if !(0.0..=200.0).contains(&p) {
            return None; // epoch 가 새어 들어온 알려진 glitch
        }
        Some(Window {
            percent: p.min(100.0),
            resets_at: self.resets_at.as_ref().and_then(parse_resets),
        })
    }
}

fn parse_resets(v: &serde_json::Value) -> Option<i64> {
    if let Some(f) = v.as_f64() {
        if f <= 0.0 {
            return None;
        }
        return Some(if f > 1e12 { (f / 1000.0) as i64 } else { f as i64 });
    }
    v.as_str().and_then(parse_rfc3339)
}

// ---------- cache ----------

#[derive(Serialize, Deserialize, Clone, Copy)]
struct Window {
    percent: f64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    resets_at: Option<i64>,
}

#[derive(Serialize, Deserialize, Default)]
struct StateFile {
    #[serde(default)]
    observed_at: i64,
    #[serde(default)]
    stdin_limits_seen: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    five_hour: Option<Window>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    seven_day: Option<Window>,
    #[serde(default)]
    alert_key: String,
    #[serde(default)]
    alert_at: i64,
}

#[derive(Deserialize, Default)]
struct UsageFile {
    #[serde(default)]
    usage: Option<Usage>,
}
#[derive(Deserialize, Default)]
struct Usage {
    #[serde(default)]
    extra: Option<Extra>,
}
#[derive(Deserialize, Default)]
struct Extra {
    #[serde(default)]
    enabled: bool,
    used_credits: Option<f64>,
    monthly_limit: Option<f64>,
}

fn read_json<T: for<'de> Deserialize<'de> + Default>(p: &PathBuf) -> T {
    std::fs::read(p)
        .ok()
        .and_then(|b| serde_json::from_slice(&b).ok())
        .unwrap_or_default()
}

/// Go 의 store.Write 와 같다: 임시 파일에 쓰고 rename.
fn write_atomic(p: &PathBuf, data: &[u8]) -> std::io::Result<()> {
    if let Some(d) = p.parent() {
        std::fs::create_dir_all(d)?;
    }
    let tmp = p.with_extension("tmp");
    {
        let mut f = std::fs::File::create(&tmp)?;
        f.write_all(data)?;
        // Go 쪽 store.Write 는 fsync 하지 않는다. 비교를 맞추려고 뺐다.
        // (cache 파일이라 유실돼도 경보가 다시 무장할 뿐이다.)
    }
    std::fs::rename(&tmp, p)
}

// ---------- 시간 ----------

fn parse_rfc3339(s: &str) -> Option<i64> {
    let b = s.as_bytes();
    if b.len() < 19 {
        return None;
    }
    let n = |a: usize, z: usize| s[a..z].parse::<i64>().ok();
    let (y, mo, d) = (n(0, 4)?, n(5, 7)?, n(8, 10)?);
    let (h, mi, sec) = (n(11, 13)?, n(14, 16)?, n(17, 19)?);
    if y == 1 {
        return None; // Go 의 zero time
    }
    Some(days_from_civil(y, mo, d) * 86400 + h * 3600 + mi * 60 + sec)
}

fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

fn fmt_rfc3339(t: i64) -> String {
    let (days, secs) = (t.div_euclid(86400), t.rem_euclid(86400));
    let (y, m, d) = civil_from_days(days);
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        y, m, d, secs / 3600, (secs % 3600) / 60, secs % 60
    )
}

fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// 로컬 타임존 오프셋(초). Go 는 stdlib 이 해주지만 Rust 는 직접 읽어야 한다.
/// TZ 규칙 전체가 아니라 현재 오프셋만 필요하므로 libc 없이 근사한다.
fn local_offset() -> i64 {
    // /etc/localtime 의 TZif 마지막 전이 오프셋을 읽는다.
    let b = match std::fs::read("/etc/localtime") {
        Ok(b) => b,
        Err(_) => return 0,
    };
    tzif_current_offset(&b).unwrap_or(0)
}

fn tzif_current_offset(b: &[u8]) -> Option<i64> {
    if b.len() < 44 || &b[0..4] != b"TZif" {
        return None;
    }
    let rd = |o: usize| i32::from_be_bytes([b[o], b[o + 1], b[o + 2], b[o + 3]]) as i64;
    let (isutc, isstd, leap, times, types) = (rd(20), rd(24), rd(28), rd(32), rd(36));
    let _ = (isutc, isstd, leap, types);
    let base = 44;
    let now = now_secs();
    let mut idx = 0usize;
    for i in 0..times as usize {
        let t = rd(base + i * 4);
        if t <= now {
            idx = b[base + times as usize * 4 + i] as usize;
        }
    }
    let ttinfo = base + times as usize * 5;
    Some(rd(ttinfo + idx * 6))
}

fn now_secs() -> i64 {
    SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs() as i64
}

// ---------- 렌더 ----------

const RESET: &str = "\x1b[0m";
const DIM: &str = "\x1b[2m";
const GREEN: &str = "\x1b[32m";
const YELLOW: &str = "\x1b[33m";
const RED: &str = "\x1b[31m";
const BOLD: &str = "\x1b[1m";
const RED_BG: &str = "\x1b[41;97m";

fn c(code: &str, text: &str) -> String {
    format!("{code}{text}{RESET}")
}

fn pct_color(p: f64) -> &'static str {
    if p >= 90.0 {
        RED
    } else if p >= 70.0 {
        YELLOW
    } else {
        GREEN
    }
}

fn duration(d: i64) -> String {
    if d < 60 {
        return "<1m".into();
    }
    let m = d / 60;
    let (days, hours, mins) = (m / 1440, (m % 1440) / 60, m % 60);
    if days > 0 {
        format!("{days}d{hours}h")
    } else if hours > 0 {
        format!("{hours}h{mins}m")
    } else {
        format!("{mins}m")
    }
}

fn reset_text(at: i64, now: i64, off: i64) -> String {
    let d = at - now;
    let mut t = duration(d);
    if d < 86400 {
        let s = (at + off).rem_euclid(86400);
        t.push_str(&format!("\u{2192}{:02}:{:02}", s / 3600, (s % 3600) / 60));
    }
    t
}

fn window_text(name: &str, w: &Window, now: i64, off: i64, alert: &Alert) -> String {
    let style = if alert.level > 0 && alert.window == name {
        if alert.burst && alert.on {
            RED_BG.to_string()
        } else {
            format!("{BOLD}{RED}")
        }
    } else {
        pct_color(w.percent).to_string()
    };
    let mut t = format!("{name} {}", c(&style, &format!("{:.0}%", w.percent)));
    if let Some(r) = w.resets_at {
        if r > now {
            t.push(' ');
            t.push_str(&c(DIM, &reset_text(r, now, off)));
        }
    }
    t
}

// ---------- 경보 ----------

#[derive(Default)]
struct Alert {
    level: u8, // 0 none / 1 near / 2 over
    window: String,
    burst: bool,
    on: bool,
    fired: bool,
}

const ALERT_BURST: i64 = 6;

fn alerts(p: &Profile, five: &Option<Window>, seven: &Option<Window>, st: &mut StateFile, now: i64) -> (Alert, bool) {
    let mut best = 0u8;
    let (mut name, mut wkey) = (String::new(), String::new());
    for (n, w) in [("7d", seven), ("5h", five)] {
        let Some(w) = w else { continue };
        let l = if w.percent >= 100.0 {
            2
        } else if p.alert_percent > 0.0 && w.percent >= p.alert_percent {
            1
        } else {
            0
        };
        if l > best {
            best = l;
            name = n.into();
            wkey = match w.resets_at {
                Some(r) => format!("{n}@{r}"),
                None => n.into(),
            };
        }
    }
    if best == 0 {
        if st.alert_key.is_empty() {
            return (Alert::default(), false);
        }
        st.alert_key.clear();
        st.alert_at = 0;
        return (Alert::default(), true);
    }
    let key = format!("{best}@{wkey}");
    let mut a = Alert { level: best, window: name, ..Default::default() };
    let mut dirty = false;
    if st.alert_key != key {
        st.alert_key = key;
        st.alert_at = now;
        dirty = true;
        a.fired = true;
    }
    let d = now - st.alert_at;
    if (0..ALERT_BURST).contains(&d) {
        a.burst = true;
        a.on = (d * 2 / 1) % 2 == 0;
    }
    (a, dirty)
}

// ---------- main ----------

fn main() {
    let now = now_secs();
    let home = std::env::var("HOME").unwrap_or_default();

    // 1) 설정
    let cfg_path = std::env::var("CC_USAGE_CONFIG")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from(&home).join(".config/cc-usage/config.json"));
    let cfg: Config = read_json(&cfg_path);
    let pname = if cfg.profiles.contains_key(&cfg.default_profile) {
        cfg.default_profile.clone()
    } else {
        cfg.profiles.keys().next().cloned().unwrap_or_else(|| "default".into())
    };
    let mut prof = cfg.profiles.get(&pname).cloned().unwrap_or_default();
    prof.apply_defaults();

    // 2) stdin
    let mut raw = Vec::new();
    let _ = std::io::stdin().read_to_end(&mut raw);
    let input: Input = serde_json::from_slice(&raw).unwrap_or_default();

    // 3) cache
    let cache_base = std::env::var("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from(&home).join(".cache"));
    let dir = cache_base.join("cc-usage").join(&pname);
    let state_path = dir.join("state.json");
    let mut st: StateFile = read_json(&state_path);
    let uf: UsageFile = read_json(&dir.join("usage.json"));

    // 4) 한도 병합
    let (mut five, mut seven) = (None, None);
    if let Some(rl) = &input.rate_limits {
        five = rl.five_hour.as_ref().and_then(|w| w.convert());
        seven = rl.seven_day.as_ref().and_then(|w| w.convert());
    }
    let stdin_present = five.is_some() || seven.is_some();
    let mut dirty = false;
    if stdin_present {
        st.observed_at = now;
        st.stdin_limits_seen = now;
        st.five_hour = five;
        st.seven_day = seven;
        dirty = true;
    } else if now - st.observed_at < 6 * 3600 {
        five = st.five_hour;
        seven = st.seven_day;
    }
    let drop_expired = |w: Option<Window>| -> Option<Window> {
        w.filter(|w| w.resets_at.map_or(true, |r| r > now))
    };
    let (five, seven) = (drop_expired(five), drop_expired(seven));

    // 5) 경보 + state 쓰기 (Go 와 같은 순서: 기록 후 발사)
    let (alert, adirty) = alerts(&prof, &five, &seven, &mut st, now);
    if dirty || adirty {
        let _ = write_atomic(&state_path, &serde_json::to_vec(&st).unwrap_or_default());
    }

    // 6) 렌더
    let off = local_offset();
    let mut parts: Vec<String> = Vec::new();
    let label = prof.status_label(&pname);
    if !label.is_empty() {
        parts.push(c(DIM, &format!("[{label}]")));
    }
    if !input.model.display_name.is_empty() {
        parts.push(input.model.display_name.clone());
    }
    if let Some(p) = input.context_window.used_percentage {
        parts.push(format!("ctx {}", c(pct_color(p), &format!("{p:.0}%"))));
    }
    if let Some(w) = &five {
        parts.push(window_text("5h", w, now, off, &alert));
    }
    if let Some(w) = &seven {
        parts.push(window_text("7d", w, now, off, &alert));
    }
    let mut lines: Vec<String> = Vec::new();
    let row = parts.join(&c(DIM, " \u{b7} "));
    if !row.is_empty() {
        lines.push(row);
    }
    if let Some(e) = uf.usage.as_ref().and_then(|u| u.extra.as_ref()) {
        if e.enabled && alert.level == 2 {
            if let Some(used) = e.used_credits {
                let mut t = format!("\u{1f4b3} ${:.2}", used / 100.0);
                if let Some(l) = e.monthly_limit {
                    t.push_str(&format!(" / ${:.2}", l / 100.0));
                }
                lines.push(c(DIM, &t));
            }
        }
    }
    if lines.is_empty() {
        lines.push(c(DIM, &format!("[{pname}]")));
    }
    let _ = fmt_rfc3339(now); // Go 가 state 에 RFC3339 를 쓰는 비용과 맞추기 위해
    println!("{}", lines.join("\n"));
}
