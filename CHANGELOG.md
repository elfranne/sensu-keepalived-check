# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic
Versioning](http://semver.org/spec/v2.0.0.html).

## Unreleased

## [0.2.0] - 2026-07-24

### Added
- Emit `keepalived_bad_state_seconds` perfdata: how long the instance has been in a state
  other than `--state`, `0` while it matches. It graphs time-in-a-bad-state directly from
  a single metric under InfluxQL 1.x. Documented an example Grafana / InfluxQL dashboard
  query in the README.

### Removed
- The check no longer emits `exit_code`, `seconds_in_state`, or `last_transition` as
  perfdata; `keepalived_bad_state_seconds` is the only metric. The check result
  (`OK`/`WARNING`/`CRITICAL` status and process exit code) is unchanged.

## [0.1.2] - 2026-07-10

### Fixed
- Corrected the Bonsai asset definitions.

## [0.1.1] - 2026-07-10

### Changed
- Removed unsupported operating systems from the goreleaser build matrix.

## [0.1.0] - 2026-07-10

### Added
- Initial release: keepalived VRRP state check for Sensu (Linux only).
