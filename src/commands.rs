//! `cc-usage statusline` — 줄을 그리는 입구. 판정·렌더는 `limits` · `statusline`, 캐시는 `statusline_cache`.

/// `statusline [--source S]` — 경로·git 줄과 모델·ctx·한도 줄을 CLI 가 직접 그린다(골든
/// `tests/fixtures/statusline-full/`). 네트워크를 기다리지 않는다. 그 아래로 `extraCommands`
/// 의 줄(설정 순서)이 붙고, 늦거나 실패하면 그 줄만 빠진다. 하위 프로세스는 렌더와 나란히 돈다(가장 느린 것 하나만
/// 기다린다). 설정(`cfg`)은 부르는 쪽이 읽어 넘긴다 — cc-usage 는 `config::load_user()`, 라이브러리로 싣는 실행 파일은
/// 자기 설정 파일의 블록(`config::from_block`).
///
/// `source` 는 이번 실행에서만 설정의 `source` 를 바꾼다(`--source`, 환경 변수 `profile::get().source_env` 보다 이긴다).
/// 모르는 값이면 무엇이 틀렸는지 한 줄만 낸다 — statusline 은 비지 않는다. `none` 이거나 Antigravity(`agy`)가 부른
/// 것이면 Claude 쪽(토큰·API·캐시·계정 파일)을 건드리지 않는다(`limits::local_limits`).
///
/// 다른 statusline 의 `extraCommands` 로 불렸으면(`bounded::nested_env()`) 아무것도 그리지 않는다 — 자기를 끝없이 다시 띄운다.
pub fn cmd_statusline(source: Option<&str>, mut cfg: crate::config::StatuslineConfig) {
    use crate::limits::{local_limits, override_source, Input};
    use crate::statusline::extra::Vars;
    use crate::statusline::full::{lines, Style, View};

    let profile = crate::profile::get();
    if std::env::var_os(profile.nested_env).is_some() {
        return;
    }
    let input = Input::parse(&statusline_stdin().unwrap_or_default());
    let env_source = std::env::var(profile.source_env).ok();
    match override_source(cfg.limits.source, env_source.as_deref(), source) {
        Ok(source) => cfg.limits.source = source,
        Err(e) => {
            println!("[{}] {e}", profile.name);
            return;
        }
    }
    let now = statusline_now();
    let vars = Vars {
        session_id: &input.session_id,
        cwd: input.dir(),
    };
    let out = std::thread::scope(|scope| {
        let extras: Vec<_> = cfg
            .extra_commands
            .iter()
            .map(|c| scope.spawn(move || crate::bounded::run_extra(c, vars).1.into_lines()))
            .collect();

        let limits_cfg = &cfg.limits;
        let local = local_limits(limits_cfg, &input, now);
        let observed = match local {
            Some(_) => None,
            None => {
                crate::statusline_cache::observe(limits_cfg, cfg.config_dir.as_deref(), &input, now)
            }
        };
        let limits = local.map_or_else(
            || observed.as_ref().map(|o| o.limits).unwrap_or_default(),
            |(lim, _)| lim,
        );
        let cache = observed.as_ref().map(|o| &o.usage);
        let badge = observed
            .as_ref()
            .and_then(|o| o.email.as_deref())
            .and_then(|email| cfg.badges.get(email));
        let home = std::env::var("HOME").ok();
        let git = crate::git_status::read(input.dir(), crate::git_status::GIT_TIMEOUT);
        let view = View {
            dir: input.dir(),
            git: git.as_ref(),
            model: &input.model,
            effort: &input.effort,
            context_pct: input.context_pct,
            limits,
            usage: cache,
            alert: local.map_or_else(
                || observed.as_ref().map(|o| o.alert).unwrap_or_default(),
                |(_, alert)| alert,
            ),
            credits: observed.as_ref().map(|o| o.credits).unwrap_or_default(),
            currency: limits_cfg.currency(),
            badge,
            credit_glow: observed.as_ref().and_then(|o| o.credit_glow),
            home: home.as_deref(),
            dir_link: cfg.dir_link.as_deref(),
            project_dir: &input.project_dir,
            now,
        };
        let style = Style::from_env(|k| std::env::var(k).ok());
        // 다른 도구의 줄은 바이트 그대로 붙여야 해서(UTF-8 이 아닐 수 있다) 줄을 바이트로 모은다.
        let mut out: Vec<Vec<u8>> = lines(&view, &style, &chrono::Local)
            .into_iter()
            .map(String::into_bytes)
            .collect();
        for extra in extras {
            out.extend(extra.join().unwrap_or_default());
        }
        out
    });
    let mut bytes = out.join(&b'\n');
    bytes.push(b'\n');
    use std::io::Write;
    let _ = std::io::stdout().write_all(&bytes);
}

/// 지금 시각. 테스트 전용 환경 변수(`profile::get().now_env`, cc-usage 는 `CC_USAGE_NOW`)에 RFC3339 시각을 주면 "지금" 을
/// 고정한다(골든 대조용).
pub fn statusline_now() -> chrono::DateTime<chrono::Utc> {
    std::env::var(crate::profile::get().now_env)
        .ok()
        .and_then(|v| chrono::DateTime::parse_from_rfc3339(&v).ok())
        .map_or_else(chrono::Utc::now, |t| t.with_timezone(&chrono::Utc))
}

/// Claude Code 의 statusline 입력 원문 — 터미널에서 그냥 부르면(stdin 이 TTY) 읽지 않는다. 4MB 까지만 읽는다.
fn statusline_stdin() -> Option<String> {
    use std::io::{IsTerminal, Read};
    if std::io::stdin().is_terminal() {
        return None;
    }
    let mut raw = String::new();
    std::io::stdin()
        .take(4 << 20)
        .read_to_string(&mut raw)
        .ok()?;
    Some(raw)
}
