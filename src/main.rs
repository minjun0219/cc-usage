//! `cc-usage` 진입점 — 명령을 가르고 플래그를 읽어 각 모듈로 넘긴다.

use cc_usage::{commands, config, help, statusline_doctor, statusline_guard, statusline_refresh};

fn main() {
    // Rust 런타임은 SIGPIPE 를 무시해 두므로 `cc-usage doctor | head -2` 처럼 읽는 쪽이 먼저 닫히면 `println!` 이
    // EPIPE 로 패닉한다. 셸 도구답게 조용히 죽도록 기본 동작으로 되돌린다.
    // SAFETY: signal(2) 에 상수 둘을 넘길 뿐이고, 스레드가 생기기 전 main 첫 줄에서 부른다.
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }
    let argv: Vec<String> = std::env::args().skip(1).collect();
    let Some((cmd, args)) = argv.split_first() else {
        eprint!("{}", help::full());
        std::process::exit(2);
    };
    // `<명령> --help` 는 오류가 아니라 도움말이다.
    if args.iter().any(|a| is_help(a)) {
        print!("{}", help::for_command(cmd));
        return;
    }
    if let Err(e) = run(cmd, args) {
        eprintln!("cc-usage: {e}");
        std::process::exit(1);
    }
}

fn run(cmd: &str, args: &[String]) -> Result<(), String> {
    match cmd {
        "statusline" => {
            // statusLine 명령줄이 틀려도 statusline 은 비지 않는다 — 무엇이 틀렸는지 한 줄을 낸다.
            match Flags::parse(args, &["source"], &[]) {
                Ok(flags) => commands::cmd_statusline(flags.value("source")),
                Err(e) => println!("[cc-usage] {e}"),
            }
            Ok(())
        }
        // statusline 이 detached 로 띄우는 갱신 — 사람이 부를 일은 없지만 불러도 해롭지 않다.
        "refresh" => {
            let mut cfg = config::load_user();
            // 부모 statusline 이 정한 source 를 환경 변수로 받는다(`statusline_refresh::spawn_detached`).
            let env = std::env::var(cc_usage::limits::SOURCE_ENV).ok();
            cfg.limits.source =
                cc_usage::limits::override_source(cfg.limits.source, env.as_deref(), None)
                    .unwrap_or(cfg.limits.source);
            statusline_refresh::run(&cfg, commands::statusline_now());
            Ok(())
        }
        // 막으면 exit 2 — Claude Code 가 prompt 를 버리고 stderr 를 보인다.
        "guard" => {
            Flags::parse(args, &[], &[])?;
            let cfg = config::load_user();
            if let Some(reason) = statusline_guard::check(&cfg, commands::statusline_now()) {
                eprintln!("{}", statusline_guard::block_message(&reason));
                std::process::exit(2);
            }
            Ok(())
        }
        "allow" => {
            let flags = Flags::parse(args, &[], &[])?;
            if flags.positionals.len() > 1 {
                return Err(format!(
                    "allow 는 인자를 하나만 받습니다: {:?}",
                    flags.positionals
                ));
            }
            let message = statusline_guard::allow(
                flags.positionals.first().map(String::as_str),
                commands::statusline_now(),
            )?;
            println!("{message}");
            Ok(())
        }
        "probe" => {
            Flags::parse(args, &[], &[])?;
            let body = statusline_doctor::probe(&config::load_user(), commands::statusline_now())?;
            println!("{body}");
            Ok(())
        }
        "doctor" => {
            // `--session-id` 는 Go 판의 이름 — 습관대로 불러도 된다.
            let flags = Flags::parse(args, &["session", "session-id"], &[])?;
            let session = flags.value("session").or(flags.value("session-id"));
            let path = config::path().unwrap_or_default();
            statusline_doctor::doctor(
                &config::load_user(),
                &path,
                session,
                commands::statusline_now(),
            )
        }
        "config" => {
            Flags::parse(args, &[], &[])?.no_positionals("config")?;
            print!("{}", help::config_text());
            Ok(())
        }
        "update" => {
            let flags = Flags::parse(args, &[], &["check"])?;
            flags.no_positionals("update")?;
            cc_usage::update::run(flags.switch("check"))
        }
        "version" | "--version" | "-v" => {
            println!("{}", cc_usage::VERSION);
            Ok(())
        }
        // `cc-usage help statusline` 도 `cc-usage statusline --help` 와 같게.
        "help" | "--help" | "-h" => {
            match args.first() {
                Some(c) => print!("{}", help::for_command(c)),
                None => print!("{}", help::full()),
            }
            Ok(())
        }
        _ => {
            eprint!("{}", help::full());
            std::process::exit(2);
        }
    }
}

fn is_help(a: &str) -> bool {
    matches!(a, "-h" | "-help" | "--help")
}

/// 작은 플래그 파서 — `--name value` · `--name=value`(값 플래그), `--name`(스위치), 나머지는 위치 인자.
struct Flags {
    values: Vec<(String, String)>,
    switches: Vec<String>,
    positionals: Vec<String>,
}

impl Flags {
    fn parse(args: &[String], valued: &[&str], switches: &[&str]) -> Result<Self, String> {
        let mut out = Flags {
            values: Vec::new(),
            switches: Vec::new(),
            positionals: Vec::new(),
        };
        let mut it = args.iter();
        while let Some(arg) = it.next() {
            let Some(name) = arg.strip_prefix("--") else {
                out.positionals.push(arg.clone());
                continue;
            };
            let (name, inline) = match name.split_once('=') {
                Some((n, v)) => (n, Some(v.to_string())),
                None => (name, None),
            };
            if valued.contains(&name) {
                let value = match inline {
                    Some(v) => v,
                    None => it
                        .next()
                        .cloned()
                        .ok_or_else(|| format!("flag --{name} requires a value"))?,
                };
                out.values.push((name.to_string(), value));
            } else if switches.contains(&name) && inline.is_none() {
                out.switches.push(name.to_string());
            } else {
                return Err(format!("unknown flag: {arg}"));
            }
        }
        Ok(out)
    }

    fn value(&self, name: &str) -> Option<&str> {
        self.values
            .iter()
            .rev()
            .find(|(n, _)| n == name)
            .map(|(_, v)| v.as_str())
    }

    fn switch(&self, name: &str) -> bool {
        self.switches.iter().any(|s| s == name)
    }

    fn no_positionals(&self, cmd: &str) -> Result<(), String> {
        match self.positionals.first() {
            Some(a) => Err(format!("{cmd} 는 인자를 받지 않습니다: {a:?}")),
            None => Ok(()),
        }
    }
}
