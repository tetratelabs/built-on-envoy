// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! An HTTP/1 header formatter that controls the casing of header names on the wire.
//!
//! Envoy lowercases header names. Some legacy HTTP/1 peers expect specific casing, so this
//! module can keep the casing the peer sent, apply proper casing, or force exact spellings.

use envoy_proxy_dynamic_modules_rust_sdk::header_formatter::*;
use envoy_proxy_dynamic_modules_rust_sdk::*;
use serde::Deserialize;
use std::collections::HashMap;
use std::sync::Arc;

/// How header names are written when no override applies.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum Mode {
    /// Keep the casing the peer sent. Headers Envoy adds use `added_header_case`.
    #[default]
    Preserve,
    /// Uppercase the first letter and every letter after a non-alphanumeric character.
    ProperCase,
    /// Leave names lowercase, which is Envoy's default.
    Lowercase,
}

/// Casing for headers Envoy adds, such as `x-request-id` or headers set by filters. Only used in
/// preserve mode.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum AddedHeaderCase {
    #[default]
    ProperCase,
    Lowercase,
}

#[derive(Deserialize, Debug, Default)]
#[serde(deny_unknown_fields)]
struct RawConfig {
    #[serde(default)]
    mode: Mode,
    #[serde(default)]
    added_header_case: Option<AddedHeaderCase>,
    #[serde(default)]
    overrides: HashMap<String, String>,
}

/// The parsed configuration. It is shared by every worker thread, so it is immutable.
#[derive(Debug)]
pub struct Config {
    mode: Mode,
    added_header_case: AddedHeaderCase,
    /// Lowercase header name to the exact spelling written on the wire.
    overrides: HashMap<String, String>,
}

impl Config {
    /// Parses the JSON configuration. Empty input means all defaults.
    pub fn parse(config: &[u8]) -> Result<Self, String> {
        let raw: RawConfig = if config.iter().all(u8::is_ascii_whitespace) {
            RawConfig::default()
        } else {
            serde_json::from_slice(config).map_err(|e| format!("invalid config: {e}"))?
        };

        if raw.added_header_case.is_some() && raw.mode != Mode::Preserve {
            return Err("added_header_case only applies when mode is preserve".to_string());
        }

        let mut overrides = HashMap::with_capacity(raw.overrides.len());
        for (name, spelling) in raw.overrides {
            if spelling.is_empty() || !spelling.bytes().all(is_token_char) {
                return Err(format!("override {spelling:?} is not a valid header name"));
            }
            // An override only changes casing; it must never rename the header.
            if !spelling.eq_ignore_ascii_case(&name) {
                return Err(format!(
                    "override {spelling:?} for {name:?} must differ only in casing"
                ));
            }
            if overrides
                .insert(name.to_ascii_lowercase(), spelling)
                .is_some()
            {
                return Err(format!("duplicate override for {name:?}"));
            }
        }

        Ok(Self {
            mode: raw.mode,
            added_header_case: raw.added_header_case.unwrap_or_default(),
            overrides,
        })
    }
}

/// Characters allowed in an HTTP header name (RFC 9110 `tchar`).
fn is_token_char(b: u8) -> bool {
    b.is_ascii_alphanumeric() || b"!#$%&'*+-.^_`|~".contains(&b)
}

/// Same rule as Envoy's `ProperCaseHeaderKeyFormatter`, so `proper_case` matches the built-in
/// `proper_case_words` setting.
fn proper_case(key: &str, out: &mut String) {
    out.clear();
    let mut capitalize = true;
    for c in key.chars() {
        out.push(if capitalize {
            c.to_ascii_uppercase()
        } else {
            c
        });
        capitalize = !c.is_ascii_alphanumeric();
    }
}

/// Per-message state. Envoy calls it from the one worker thread that owns the message.
struct CaseFormatter {
    config: Arc<Config>,
    /// Lowercase name to the casing the peer sent. Only filled in preserve mode.
    observed: HashMap<String, String>,
    /// Backs the value returned by `format`, which must outlive the call.
    formatted: String,
}

impl CaseFormatter {
    fn new(config: Arc<Config>) -> Self {
        Self {
            config,
            observed: HashMap::new(),
            formatted: String::new(),
        }
    }

    fn observe(&mut self, key: &str) {
        if self.config.mode == Mode::Preserve {
            self.observed
                .insert(key.to_ascii_lowercase(), key.to_string());
        }
    }

    /// Returns the spelling to write, or `None` to keep the lowercase name.
    fn format_key(&mut self, key: &str) -> Option<&str> {
        if let Some(spelling) = self.config.overrides.get(key) {
            return Some(spelling);
        }
        let casing = match self.config.mode {
            // Checked with contains_key first so the borrow of `observed` doesn't block the
            // write to `formatted` below.
            Mode::Preserve if self.observed.contains_key(key) => {
                return self.observed.get(key).map(String::as_str);
            }
            Mode::Preserve => self.config.added_header_case,
            // The other modes apply the same casing to every header, seen or not.
            Mode::ProperCase => AddedHeaderCase::ProperCase,
            Mode::Lowercase => AddedHeaderCase::Lowercase,
        };
        match casing {
            AddedHeaderCase::Lowercase => None,
            AddedHeaderCase::ProperCase => {
                proper_case(key, &mut self.formatted);
                Some(&self.formatted)
            }
        }
    }
}

impl HeaderFormatter for CaseFormatter {
    fn process_key(&mut self, key: &str, _handle: &HeaderFormatterHandle) {
        self.observe(key);
    }

    fn format(&mut self, key: &str, _handle: &HeaderFormatterHandle) -> Option<&str> {
        self.format_key(key)
    }
}

struct SharedConfig(Arc<Config>);

impl HeaderFormatterConfig for SharedConfig {
    fn create(&self) -> Option<Box<dyn HeaderFormatter>> {
        Some(Box::new(CaseFormatter::new(self.0.clone())))
    }
}

fn new_header_formatter_config(
    _name: &str,
    config: &[u8],
) -> Option<Box<dyn HeaderFormatterConfig>> {
    match Config::parse(config) {
        Ok(cfg) => Some(Box::new(SharedConfig(Arc::new(cfg)))),
        Err(err) => {
            eprintln!("header-case: {err}");
            None
        }
    }
}

fn init() -> bool {
    true
}

declare_all_init_functions!(init, header_formatter: new_header_formatter_config);

#[cfg(test)]
mod tests {
    use super::*;

    fn formatter(json: &str) -> CaseFormatter {
        CaseFormatter::new(Arc::new(Config::parse(json.as_bytes()).unwrap()))
    }

    fn format(f: &mut CaseFormatter, key: &str) -> Option<String> {
        f.format_key(key).map(str::to_string)
    }

    #[test]
    fn empty_config_uses_defaults() {
        for input in ["", "  ", "{}"] {
            let cfg = Config::parse(input.as_bytes()).unwrap();
            assert_eq!(cfg.mode, Mode::Preserve);
            assert_eq!(cfg.added_header_case, AddedHeaderCase::ProperCase);
            assert!(cfg.overrides.is_empty());
        }
    }

    #[test]
    fn rejects_bad_config() {
        for input in [
            "not json",
            r#"{"mode":"upper"}"#,
            r#"{"unknown":1}"#,
            r#"{"mode":"proper_case","added_header_case":"lowercase"}"#,
            r#"{"overrides":{"content-type":"Content-Length"}}"#,
            r#"{"overrides":{"x-a":""}}"#,
            r#"{"overrides":{"x a":"X A"}}"#,
            r#"{"overrides":{"X-A":"X-a","x-a":"x-A"}}"#,
        ] {
            assert!(Config::parse(input.as_bytes()).is_err(), "{input}");
        }
    }

    #[test]
    fn proper_case_matches_envoy() {
        let mut out = String::new();
        for (input, want) in [
            ("x-foo-bar", "X-Foo-Bar"),
            ("content-type", "Content-Type"),
            ("www-authenticate", "Www-Authenticate"),
            ("x-1st-try", "X-1st-Try"),
            ("x_under.dot", "X_Under.Dot"),
            ("a", "A"),
            ("", ""),
        ] {
            proper_case(input, &mut out);
            assert_eq!(out, want);
        }
    }

    #[test]
    fn preserve_mode() {
        let mut f = formatter(r#"{"mode":"preserve"}"#);
        f.observe("X-Legacy-ID");
        f.observe("content-TYPE");
        assert_eq!(
            format(&mut f, "x-legacy-id").as_deref(),
            Some("X-Legacy-ID")
        );
        assert_eq!(
            format(&mut f, "content-type").as_deref(),
            Some("content-TYPE")
        );
        // Never seen on the wire, so added_header_case applies.
        assert_eq!(
            format(&mut f, "x-envoy-upstream-service-time").as_deref(),
            Some("X-Envoy-Upstream-Service-Time")
        );
    }

    #[test]
    fn preserve_mode_last_casing_wins() {
        let mut f = formatter("{}");
        f.observe("x-dup");
        f.observe("X-DUP");
        assert_eq!(format(&mut f, "x-dup").as_deref(), Some("X-DUP"));
    }

    #[test]
    fn preserve_mode_lowercase_added_headers() {
        let mut f = formatter(r#"{"added_header_case":"lowercase"}"#);
        f.observe("X-Seen");
        assert_eq!(format(&mut f, "x-seen").as_deref(), Some("X-Seen"));
        assert_eq!(format(&mut f, "date"), None);
    }

    #[test]
    fn proper_case_mode_ignores_wire_casing() {
        let mut f = formatter(r#"{"mode":"proper_case"}"#);
        f.observe("X-LEGACY-ID");
        assert!(f.observed.is_empty());
        assert_eq!(
            format(&mut f, "x-legacy-id").as_deref(),
            Some("X-Legacy-Id")
        );
    }

    #[test]
    fn lowercase_mode() {
        let mut f = formatter(r#"{"mode":"lowercase"}"#);
        f.observe("X-Foo");
        assert_eq!(format(&mut f, "x-foo"), None);
    }

    #[test]
    fn overrides_win() {
        for mode in ["preserve", "proper_case", "lowercase"] {
            let mut f = formatter(&format!(
                r#"{{"mode":"{mode}","overrides":{{"Content-Type":"Content-type"}}}}"#
            ));
            f.observe("CONTENT-TYPE");
            assert_eq!(
                format(&mut f, "content-type").as_deref(),
                Some("Content-type"),
                "{mode}"
            );
        }
    }

    #[test]
    fn sdk_entry_points() {
        assert!(init());
        assert!(new_header_formatter_config("header-case", b"not json").is_none());

        let config = new_header_formatter_config(
            "header-case",
            br#"{"overrides":{"content-type":"Content-type"}}"#,
        )
        .unwrap();
        let mut f = config.create().unwrap();
        // The module never uses the handle, so a null Envoy pointer is fine here.
        let handle = unsafe { HeaderFormatterHandle::new(std::ptr::null_mut()) };
        f.process_key("X-Seen", &handle);
        assert_eq!(f.format("x-seen", &handle), Some("X-Seen"));
        assert_eq!(f.format("content-type", &handle), Some("Content-type"));
        assert_eq!(f.format("x-new", &handle), Some("X-New"));
    }

    fn schema() -> jsonschema::Validator {
        let schema: serde_json::Value =
            serde_json::from_str(include_str!("../config.schema.json")).unwrap();
        jsonschema::validator_for(&schema).unwrap()
    }

    #[test]
    fn schema_accepts_what_parse_accepts() {
        let v = schema();
        for input in [
            "{}",
            r#"{"mode":"preserve","added_header_case":"lowercase"}"#,
            r#"{"mode":"proper_case"}"#,
            r#"{"mode":"lowercase","overrides":{"content-type":"Content-type"}}"#,
        ] {
            let json: serde_json::Value = serde_json::from_str(input).unwrap();
            assert!(v.is_valid(&json), "{input}");
            assert!(Config::parse(input.as_bytes()).is_ok(), "{input}");
        }
    }

    // JSON Schema can't check that an override only changes casing, so this passes the schema and
    // is rejected when Envoy loads the config.
    #[test]
    fn schema_cannot_check_override_casing() {
        let input = r#"{"overrides":{"content-type":"Content-Length"}}"#;
        let json: serde_json::Value = serde_json::from_str(input).unwrap();
        assert!(schema().is_valid(&json));
        assert!(Config::parse(input.as_bytes()).is_err());
    }

    #[test]
    fn schema_rejects_bad_config() {
        let v = schema();
        for input in [
            r#"{"mode":"upper"}"#,
            r#"{"unknown":1}"#,
            r#"{"mode":"proper_case","added_header_case":"lowercase"}"#,
            r#"{"overrides":{"x-a":""}}"#,
            r#"{"overrides":{"x-a":"x a"}}"#,
        ] {
            let json: serde_json::Value = serde_json::from_str(input).unwrap();
            assert!(!v.is_valid(&json), "{input}");
        }
    }
}
