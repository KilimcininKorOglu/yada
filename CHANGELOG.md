# Changelog

## [1.0.0] - 2026-08-12

First release. The PowerShell script that started the project is kept working
and ships alongside the Go application.

### Added

- Command line and desktop interface built from one binary: no arguments opens the window, `-cli` or any subcommand runs the command line.
- Commands for `check`, `list`, `add`, `update`, `delete`, `import`, `export`, `diff`, `sync`, `reload` and `config`.
- Configuration file loading with defaults, validation that reports every problem at once, and rejection of unknown keys.
- Remote commands through the system `ssh` binary, so the user's own `~/.ssh/config`, agent and jump hosts keep working.
- Records file parsing and serialisation that preserves comments and unmanaged directives byte for byte.
- Writes that back up, validate with `unbound-checkconf` and roll back when validation fails.
- Four refresh tiers, lightest first: `unbound-control local_data`, `reload_keep_cache`, `systemctl reload`, `systemctl restart`.
- Runtime record push with `unbound-control local_data`, so a change reaches the daemon without a config re-read or a cache loss.
- Comparison across servers and a sync that copies missing records, leaving conflicting values for the user to resolve.
- CSV import and export, with per-line reporting of the rows that failed validation.
- Fyne desktop interface with six tabs: Sunucular, Kayıtlar, Fark, Toplu İşlem, Ayarlar, Günlük.
- Configuration editing from the settings tab, validated before it reaches the disk.
- A form that adds a server and writes the private key file, the `known_hosts` entry and the `Host` block in the ssh configuration, showing the host key fingerprint before it is trusted.
- Configuration editing that preserves comments and the defaults block, through the YAML node tree.
- A conflict check before an add: an identical record is reported, a different value is offered for editing, and one decision brings every server to the same state.
- Docker test stack with three real Unbound servers and an end-to-end scenario asserted against what the resolvers answer.
- Build, test and release workflows, and cross compilation of the static command line build for five platforms.

### Changed

- Configuration edits and record writes travel over stdin, never inside a remote command line.
- Servers and the diff source picker are listed from the configuration rather than from a network result, so both are populated before anything is tested.
- The project is named yada and published at `github.com/KilimcininKorOglu/yada`.
- Sample addresses use the RFC 5737 documentation range.
- Go 1.26 idioms adopted throughout, and every `gosec` finding either fixed or annotated with its justification.

### Fixed

- `golang.org/x/image` upgraded past the tiff decoder CVEs, ahead of the Fyne release that pins it.
- The desktop interface opens when only its own flags are given, instead of printing the command line help.
- A duplicate server is detected on host and port together, so the same host on two ports is not rejected.
- TXT data is quoted on write, and a bare TLD is never declared as the zone.
- The Linux interface build installs the Wayland headers glfw compiles against unconditionally.
