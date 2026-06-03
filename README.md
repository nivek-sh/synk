synk
====

`synk` generates an OpenSSH config from Bitwarden items using ordered profiles.
It is meant for setups where `general -> personal -> work` should resolve to one
effective `Host` block per alias.

Status: early v1 implementation.

## How it works

Store SSH data in Bitwarden SSH key items. `synk` filters Bitwarden items by
the CLI item type for SSH keys (`type=5`) and ignores normal logins, cards,
identities, and secure notes. Custom fields use the real OpenSSH directive
names, without a `ssh_` prefix.

### synk fields

| Field | Required | Meaning |
| --- | --- | --- |
| `Enabled` | yes | Must be the string `true` for `synk` to track the item |
| `Host` | no | OpenSSH `Host` alias. Defaults to the Bitwarden item name slug |
| `Profiles` | no | Comma-separated profile list. Defaults to `general` |

### SSH directives

| Field | Required | Meaning |
| --- | --- | --- |
| `HostName` | yes | Real host, IP, or DNS name to connect to |
| `User` | yes | SSH username for the host |
| `Port` | no | SSH port |
| `IdentityFile` | no | Local identity file path, only emitted when explicitly configured |
| `IdentitiesOnly` | no | Restrict auth to configured identity files |
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

## Commands

```sh
synk init
synk doctor
synk profile status
synk profile edit
synk profile move pro before nk
synk profile move pro down
synk profile add pro
synk profile remove pro
synk profile show
synk profile set general,nk,pro
synk apply --dry-run
synk apply --sync
synk list
```

`synk init` creates `~/.config/synk/config.toml`, installs an idempotent
`Include ~/.ssh/config.d/synk.conf` line in `~/.ssh/config`, and creates the
managed config file.

By default, `synk apply` writes:

- `~/.ssh/config.d/synk.conf`

Use `--stdout` or `--dry-run` to print without writing.

`synk` never writes private keys or public keys. If you need `IdentityFile`, add
it explicitly as a Bitwarden custom field.

If the Bitwarden CLI is logged in but locked, commands that read the vault run
`bw unlock --raw` automatically and let `bw` show its normal password prompt.
The returned session token is cached in a private runtime file so later `synk`
commands can skip `bw status` and reuse it until the local TTL expires. The
cache lives under `XDG_RUNTIME_DIR/synk/bw-session.json` or `/tmp/synk-$UID/`,
uses file mode `0600`, is never written to `config.toml`, and is deleted if `bw`
rejects it.

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

Inside the editor:

- `j/k` or arrow keys move the cursor.
- `J/K` move the selected profile down/up.
- `space` enables or disables a discovered profile.
- `enter` saves.
- `q` cancels.

The editor and `synk profile status` color profiles by override state when the
terminal supports ANSI colors:

- green: no hosts from that profile are overridden by later profiles.
- yellow: some hosts are overridden.
- red: every host in that profile is overridden.
- dim: profile has no active hosts or is inactive.

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
go test ./...
go vet ./...
nix flake check
```

The Nix dev shell includes Go tooling, `bitwarden-cli`, `git`, `just`, and a
local wrapper that runs the current source tree with `go run`.

On a fresh Git repo, run `git add .` before `nix flake check` or `nix build` so
Nix can see the Go sources.
