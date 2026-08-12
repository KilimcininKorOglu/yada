# Changelog

## [1.0.2] - 2026-08-12

### Changed

- The interface is no longer built on three runners in CI. Its tests move to the lint job, which already has the cgo build dependencies, and the release workflow still builds it for every target.

### Fixed

- A failed operation reports what went wrong. The interface cancelled its own context as soon as the work returned and then asked that context whether the user had cancelled, so every failure was logged as a cancellation and the real message, in the log and in the error dialog, was lost.
- Adding a server validates the configuration before it writes anything. A server the configuration file rejects, such as one repeating a host and port already listed, no longer leaves a private key, a `known_hosts` entry and a `Host` block behind for a server that never appears in the application.

## [1.0.1] - 2026-08-12

### Changed

- The release body now leads with the CHANGELOG section for the tag, followed by the description of the published files.
- The README rewritten: real artifact names and their checksum file, the full Linux build dependency list, the Docker requirement of the interface cross build, every command flag, the six interface tabs, the sudo the target servers need, and the version the binary reports.
- Technical terms are kept in their own language across the Turkish documentation and test messages.
- The CI cross-compile job no longer uploads artifacts; it exists to compile the platform specific code, and the published binaries come from the release workflow.
- The file permission test is skipped on Windows, where Go reports `0666` for every file and the permission lives in an ACL.

### Fixed

- A cancelled remote command returns instead of waiting for the process `ssh` leaves behind. Output is collected through a pipe that this process keeps open, which on Linux held the caller until the command finished on its own.

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
