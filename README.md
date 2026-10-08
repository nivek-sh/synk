synk
====

`synk` generates an OpenSSH config from Bitwarden items using ordered profiles.
It is meant for setups where `general -> personal -> work` should resolve to one
effective `Host` block per alias.

Status: early development.

## Installation

Runtime dependencies are the Bitwarden CLI (`bw`) and the OpenSSH client
(`ssh`).

### Arch Linux (AUR)

```sh
paru -S synk
```

### GitHub release archive

Download the archive for your platform from the
[latest GitHub release](https://github.com/nivek-sh/synk/releases/latest):

- `linux-x86_64` or `linux-arm64`
- `macos-x86_64` or `macos-arm64`

The standalone `install.sh` asset detects the current platform, downloads the
matching archive from that release, verifies it against `SHA256SUMS`, and
installs it:

```sh
curl -fsSLO https://github.com/nivek-sh/synk/releases/latest/download/install.sh
bash install.sh
```

The same installer is also bundled inside every platform archive, so the
download can be inspected and installed entirely offline after extraction.

The default destination is `~/.local/bin`. For a system-wide installation:

```sh
sudo ./install.sh --system
```

Use `./install.sh --dir PATH` or `SYNK_INSTALL_DIR=PATH ./install.sh` for a
custom destination. The installer only copies the bundled `synk` binary; it
does not modify SSH configuration. Run `synk init` explicitly when ready.

### Nix

Run or install a pinned release directly from its Git tag:

```sh
nix run github:nivek-sh/synk/v0.4.1 -- --version
nix profile install github:nivek-sh/synk/v0.4.1
```

## How it works

Store SSH data in Bitwarden SSH key items. For a key Bitwarden's SSH agent does
not support, use a secure note (`type=2`) with a checked Boolean custom field
named `Legacy SSH` and a `Private Key` custom field containing the complete
private key. Other secure notes, logins, cards, and identities are ignored.
Custom fields use the real OpenSSH directive names, without a `ssh_` prefix.

### synk fields

| Field | Required | Meaning |
| --- | --- | --- |
| `Enabled` | yes | Must be the string `true` for `synk` to track the item |
| `Host` | no | OpenSSH `Host` alias. Defaults to the Bitwarden item name slug |
| `Profiles` | no | Comma-separated profile list. Defaults to `general` |

Legacy SSH notes also require `Legacy SSH` (a checked Boolean field) and
`Private Key` (preferably a Hidden field). Paste the complete armored key,
including its BEGIN/END markers. If the custom field turns the key into one
line, `synk` restores the line breaks around its base64 content before writing
the identity file. Multiline PEM keys with extra encryption headers must retain
their line breaks. `Enabled`, `HostName`, and `User` are still required. Do not
set `IdentityFile` on a legacy SSH note: `synk` creates the identity file from
`Private Key`.

### SSH directives

| Field | Required | Meaning |
| --- | --- | --- |
| `HostName` | yes | Real host, IP, or DNS name to connect to |
| `User` | yes | SSH username for the host |
| `Port` | no | SSH port |
| `IdentityFile` | no | Explicit local identity path for native SSH key items. If omitted, synk manages the Bitwarden public key automatically |
| `IdentitiesOnly` | no | Restrict auth to configured identities. Defaults to `yes` for managed Bitwarden keys |
| `ProxyJump` | no | Jump host chain |
| `ProxyCommand` | no | Custom proxy command |
| `ForwardAgent` | no | Enable or disable agent forwarding |
| `ServerAliveInterval` | no | Keepalive interval in seconds |
| `ServerAliveCountMax` | no | Keepalive failure threshold |
| `AddKeysToAgent` | no | Ask OpenSSH to add keys to the agent |
| `UseKeychain` | no | macOS OpenSSH keychain integration |

Examples:

```text
Bitwarden item name: app-server

Enabled    true
HostName    203.0.113.10
User        deploy
Profiles    general
```

That renders as `Host app-server`. If an item is named `Like This`,
the generated host alias is `like-this` unless the `Host` custom
field is set explicitly.

`Enabled`, `HostName`, and `User` are required for `synk` to track an item.
Only the string `true` enables tracking. The string `false`, missing, empty,
`null`, boolean values, or any other value are ignored. All other supported SSH
directives are optional. Bitwarden item notes are emitted as comments inside the
generated `Host` block.

Use `Profiles`, plural, for profile assignment. `Profile` is not supported.

For an effective Bitwarden SSH key item without an explicit `IdentityFile`,
`synk` writes the item's native public key under
`~/.ssh/config.d/synk.keys/` and points OpenSSH to that `.pub` file with
`IdentitiesOnly yes`. This lets OpenSSH select the matching private key from
the Bitwarden SSH agent without writing private key material to disk.

For an effective legacy SSH note, `synk` writes the private key to
`~/.ssh/config.d/synk.keys/bw-<item-id>.key` and sets `IdentityFile` to that
path. The directory uses mode `0700` and the file uses mode `0600`. Only
effective notes are written; a later `synk apply` removes a managed private
key when its note is disabled, removed, or overridden. `synk disable` retains
the generated config and managed keys so `synk init` can re-enable them.

## Commands

```sh
synk init
synk disable
synk doctor
synk show
synk preview
synk status
synk diff
synk list
synk list --all
synk apply
synk profile list
synk profile explain
synk profile explain dev
synk profile edit
synk profile edit --local
synk profile edit --refresh manual
synk profile edit --no-sync
synk profile move pro before nk
synk profile move pro down
synk profile add pro
synk profile remove pro
synk profile show
synk profile set general,nk,pro
synk cache refresh
synk cache refresh --no-sync
```

`synk init` creates `~/.config/synk/config.toml`, installs an idempotent
`Include ~/.ssh/config.d/synk.conf` line in `~/.ssh/config`, and creates the
managed config file. If all three already exist, it reports that synk is
initialized and does not rewrite anything. If the installation is partial, it
repairs only the missing pieces.

`synk disable` removes only that `Include` line after backing up the user SSH
config. The synk config, generated SSH config, and managed keys are retained;
run `synk init` to enable the integration again.

By default, `synk apply` manages:

- `~/.ssh/config.d/synk.conf`
- `~/.ssh/config.d/synk.keys/bw-<item-id>.pub`
- `~/.ssh/config.d/synk.keys/bw-<item-id>.key` for effective legacy SSH notes

`apply` always runs `bw sync` first. If syncing, validation, or key preparation
fails, the managed SSH config is not replaced. Managed keys are written before
the config so the config never points to a missing managed key.
Files whose content and permissions are already correct are kept in place.
In an interactive terminal, `apply` displays a staged progress bar on stderr;
use `synk apply --no-progress` to disable it. Redirected output and CI runs do
not render the progress bar.

Use the inspection commands to distinguish installed and desired state:

- `synk show` prints the currently installed managed config without contacting
  Bitwarden.
- `synk preview` reads Bitwarden and prints exactly what `apply` would install,
  without writing files.
- `synk status` reports additions, updates, removals, and missing or obsolete
  managed keys.
- `synk diff` prints the installed-to-desired config diff and managed-key
  repairs. It never prints private-key contents.
- `synk list` lists effective hosts after profile precedence is resolved.
- `synk list --all` also shows inactive and overridden Bitwarden candidates.

`apply` always runs `bw sync` and reads the vault on every invocation, so it
detects remote changes immediately. Other vault-reading commands (`preview`,
`status`, `diff`, `list`, `profile list`, and `profile explain`) reuse a successful
sync from the same Bitwarden session for up to 15 seconds. They still read the
vault on every invocation. Use `synk --force-sync status` (or place the flag
after the command) to fetch remote changes immediately. The old `--sync`
flags on `list` and deprecated profile aliases also force a sync. The old
`apply --stdout` and `apply --dry-run` interfaces remain as deprecated
compatibility options.

`synk cache refresh` uses the same 15-second sync window before reading items.
Use `synk --force-sync cache refresh` to sync immediately, or
`synk cache refresh --no-sync` to skip syncing even when the last sync was
older. `--force-sync` and `--no-sync` cannot be combined.

Native SSH key items never write private keys. Legacy SSH notes do write a
local private-key copy as described above. Managed keys use mode `0600` inside
a `0700` directory. An explicit `IdentityFile` takes precedence over automatic
Bitwarden public-key management for native SSH key items.

The managed locations can be changed in `config.toml`:

```toml
managed_config_path = "~/.ssh/config.d/synk.conf"
managed_keys_path = "~/.ssh/config.d/synk.keys"
```

If the Bitwarden CLI is logged in but locked, commands that read the vault run
`bw unlock --raw` automatically and let `bw` show its normal password prompt.
The returned session token is cached in a private runtime file so later `synk`
commands can skip `bw status` and reuse it until the local TTL expires. The
cache lives under `XDG_RUNTIME_DIR/synk/bw-session.json` or `/tmp/synk-$UID/`,
uses file mode `0600`, is never written to `config.toml`, and is deleted if `bw`
rejects it.

The recent-sync marker is stored alongside the session cache as `bw-sync.json`
with mode `0600`. It contains only a timestamp and a digest tied to the active
session and Bitwarden executable. Without a session token, commands sync every
time.

The default cache TTL is 8 hours. Override it with:

```sh
export SYNK_BW_SESSION_TTL=1h
```

Use `0s` to disable TTL expiry for the runtime cache; `bw` can still reject the
session, and `synk` will unlock again.

## Profile precedence

The `general` profile is always active first. Later profiles override earlier
profiles by exact `Host` match:

```sh
synk profile show
# general,nk,pro
```

In that example, `pro` wins over `nk`, and `nk` wins over `general`.

Use the interactive editor when you do not want to type the full profile list:

```sh
synk profile edit
```

The editor opens immediately from `config.toml` plus the local profile cache,
then starts a detached background cache refresh. When fresh host/profile
metadata is ready, the editor updates discovered profiles and override colors
without resetting your current cursor, order, or enabled profiles. If you leave
the editor before the refresh finishes, the detached refresh can still complete
and update the cache for the next run.

The profile cache stores only metadata needed by the editor: `Host`, `Profile`,
and source item name. It does not store SSH directives, keys, passwords, notes,
or raw Bitwarden JSON.

Refresh behavior is configured in `~/.config/synk/config.toml`:

```toml
[profile_editor]
refresh = "auto"
```

Supported values:

- `auto`: check Bitwarden in the UI, open `bw unlock --raw` automatically if
  the vault is locked, then start a detached cache refresh.
- `manual`: check Bitwarden in the UI; if the vault is locked, press `u` inside
  the editor to unlock and start the detached refresh.
- `never`: never contact Bitwarden from the editor; use the local cache only.

Override the config per run:

```sh
synk profile edit --refresh manual
synk profile edit --refresh never
```

For a fast local-only editor, use the shorthand:

```sh
synk profile edit --local
```

Refresh the cache manually:

```sh
synk cache refresh
```

The editor uses the same command with `--background` for detached refreshes.
Background refreshes never prompt directly; the editor handles unlock prompts
before launching them. Editor refreshes use the 15-second sync window by
default; use `synk profile edit --no-sync` to skip sync for that background
refresh or `synk profile edit --force-sync` to force it.

Inside the editor:

- `j/k` or arrow keys move the cursor.
- `J/K` move the selected profile down/up.
- `space` enables or disables a discovered profile.
- `r` refreshes Bitwarden.
- `u` unlocks Bitwarden and refreshes.
- `enter` saves.
- `q` cancels.

The editor and `synk profile list` color profiles by override state when the
terminal supports ANSI colors:

- green: no hosts from that profile are overridden by later profiles.
- yellow: some hosts are overridden.
- red: every host in that profile is overridden.
- dim: profile has no active hosts or is inactive.

The editor and `synk profile list` keep the `OVERRIDE` column compact with
counts only. Use `synk profile explain` when you want to inspect why one entry
wins over another:

```sh
synk profile explain
synk profile explain dev
```

`profile explain` renders OpenSSH-style `Host` blocks in the same order as the
managed config:

- `=` marks a block that is kept and has no competing profile.
- `+` marks the winning block for a `Host` that overrides earlier profiles.
- `-` marks blocks that are discarded because a later profile wins.

It uses the 15-second sync window. Use `--no-color` for plain output. The old
`profile status` and `profile diff` names remain as deprecated aliases for
`profile list` and `profile explain`.

For scripts or quick edits, move one profile at a time:

```sh
synk profile move pro before nk
synk profile move pro after general
synk profile move pro up
synk profile move pro down
synk profile move pro top
synk profile move pro bottom
```

`profile set` still exists for direct/manual edits, but it is not the
recommended workflow once you have many profiles.

## Development

```sh
nix develop
synk --help
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
nix flake check
```

The Nix dev shell includes Go tooling, `bitwarden-cli`, `git`, `just`, and a
local wrapper that runs the current source tree with `go run`.

On a fresh Git repo, run `git add .` before `nix flake check` or `nix build` so
Nix can see the Go sources.

## Publishing a release

The `Release` GitHub Actions workflow runs for stable tags matching
`vMAJOR.MINOR.PATCH`. It:

1. runs all Go tests and `go vet`;
2. builds Linux and macOS archives for x86-64 and ARM64;
3. creates a deterministic source archive, standalone installer, and
   `SHA256SUMS`;
4. creates a GitHub Release with generated release notes;
5. validates the AUR package in an Arch Linux container and pushes its updated
   `PKGBUILD` and `.SRCINFO`.

Before tagging, update both version occurrences in `flake.nix` and `pkgver` in
`packaging/aur/PKGBUILD`, then run:

```sh
git add -A
just check
git commit -m "fix: release v0.4.1"
git tag -a v0.4.1 -m "synk v0.4.1"
git push --atomic origin master v0.4.1
```

The tag, `flake.nix`, and the AUR template must declare the same version or the
workflow stops before publishing.

### GitHub Actions credentials

GitHub Releases use the automatically provided `GITHUB_TOKEN`; no personal
access token is needed. The workflow grants it only `contents: write` in the
release job.

Publishing to AUR needs one repository secret named
`AUR_SSH_PRIVATE_KEY`. Create a dedicated unencrypted key for this automation:

```sh
ssh-keygen -t ed25519 -N '' -f ~/.ssh/synk-aur-actions -C synk-github-actions
```

Append `~/.ssh/synk-aur-actions.pub` to the SSH public keys in the AUR account,
then store the complete contents of `~/.ssh/synk-aur-actions` in GitHub under
`Settings -> Secrets and variables -> Actions -> New repository secret`.
The dedicated key can be revoked without affecting personal SSH keys.

The optional repository variables `AUR_GIT_NAME` and `AUR_GIT_EMAIL` control
the AUR commit author. They default to the current maintainer identity.

To create release assets for an existing tag, open `Actions -> Release -> Run
workflow`, enter the tag, and leave `publish_aur` disabled. This is the intended
way to create the first GitHub Release for the existing `v0.3.0` tag without
republishing AUR.
