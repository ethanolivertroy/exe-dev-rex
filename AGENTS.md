# Setup guide for coding agents

You are setting up **exe.dev VMs as Rex remote hosts** on your human's Mac. When you're done, `exe-rex new <name>` creates a VM that shows up in the Rex app's sidebar about 10 seconds later, and `exe-rex rm <name>` removes it everywhere. Read `README.md` for the design, and `docs/details.md` for why each piece exists.

Every step ends with a **Check**. Don't go on until it passes.

## Ground rules

- **Use `--json` to read results.** `exe-rex ls|new|rm|install|doctor|config --json` print one JSON object on stdout (`"ok"` plus fields; see README, *Scripting with --json*) and never prompt.
- **Use the commands, not the TUI.** `exe-rex` on its own opens a full-screen TUI meant for the human; mention it to them when you report back. Everything in it is also a command (`ls`, `new`, `rm`, `install`, `doctor`, `config`).
- **Read the code before you run it.** `internal/ops/install.go` is the installer, and `exe-setup.sh` runs on every new VM.
- **Never ask for, read, or handle the Tailscale API token.** The installer asks for it with hidden input. The human runs that step and pastes the token into the installer, not into the chat.
- **The installer and `exe-rex new`/`rm` quit and reopen the Rex app.** Tell the human before the first time. Sessions survive, because they live in the separate Rex Server process, and so does your process if you're in a Rex terminal.
- **The test creates and deletes a real exe.dev VM** (step 4). Say so first.

## 0. Prerequisites

```bash
ls -d "/Applications/Rex"*.app              # Rex Mac app
rex version                                 # rex CLI on PATH
go version                                  # go1.27.3 or newer
tailscale status --json | jq -r .BackendState   # Running
ssh -o BatchMode=yes exe.dev whoami         # exe.dev SSH works
```

**Check:** all succeed. If Go is older than 1.27.3, ask the human to update it from https://go.dev/dl/ (older toolchains have known vulnerabilities, and `go.mod` refuses them).

## 1. Build

```bash
git clone https://github.com/ethanolivertroy/exe-dev-rex ~/exe-dev-rex
cd ~/exe-dev-rex
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -o exe-rex ./cmd/exe-rex
```

**Check:** govulncheck says `No vulnerabilities found`, and the tests pass.

## 2. Dry run

```bash
./exe-rex install --dry-run
```

**Check:** it exits 0. Show the human what it would change. If it says the Rex host list format is unknown, stop: this Rex version stores hosts differently, and `exe-rex new` would fail at its last step.

## 3. Install (the human runs it)

If the dry run shows Tailscale setup is needed, the installer will ask for an API access token. Ask the human to run it themselves, in their terminal or with `! ~/exe-dev-rex/exe-rex install` in Claude Code:

```bash
~/exe-dev-rex/exe-rex install
```

They'll create the token at https://login.tailscale.com/admin/settings/keys, paste it when asked, review the policy diff, and can revoke the token afterwards. By default VMs join as ephemeral Tailscale nodes and the trust credential gets `auth_keys` only. If the human wants nodes that stay while a VM is off for long, they add `--persistent`; read them the installer's `devices:core` warning first (the choice is theirs). Mention the ephemeral trade-off from README.md: a VM off the tailnet for 30–60+ minutes rejoins by itself, but that restarts Rex there and stops programs running in its sessions. If nothing needs a token (the integration already exists), you may run `./exe-rex install --yes` yourself.

**Check:** `exe-rex doctor` reports no `✗` lines. Its `!` lines are warnings; pass them on to the human.

## 4. End-to-end test

Tell the human you're about to create and delete a test VM, and that Rex will restart twice.

```bash
exe-rex new agent-setup-test
rex-app-host list | grep agent-setup-test     # in the sidebar list
rex hosts check | grep agent-setup-test       # reachable
exe-rex rm -y agent-setup-test
```

**Check:** `rm` prints `✓ Removed its Tailscale device` (or `✓ Deleted its Tailscale device` for persistent nodes) and ends with `● agent-setup-test is gone`, and afterwards these are all empty:

```bash
rex-app-host list | grep agent-setup-test
rex hosts list | grep agent-setup-test
ssh exe.dev ls | grep agent-setup-test
tailscale status | grep agent-setup-test
```

The run's trace is in `~/Library/Logs/exe-dev/exe-new-agent-setup-test-*.log`.

## Existing VMs (optional)

`exe-setup.sh` only runs at first boot. For a VM that already exists:

```bash
ssh -o StrictHostKeyChecking=accept-new <vm>.exe.xyz 'bash -s' < ~/exe-dev-rex/exe-setup.sh
rex-app-host add "<vm> - exe.dev" <vm>.<tailnet>.ts.net
```

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The VM never joins the tailnet | Read `/tmp/exe-setup.log` on the VM. Run `exe-rex doctor`: usually the integration is pending, or the VM lacks the exe.dev tag the integration is attached to (`rex-host` by default; `ssh exe.dev tag <vm> rex-host`). |
| On the tailnet, but Rex is unreachable | The `tag:exe` grant for `tcp:7297` is missing. `TS_API_KEY=… exe-rex doctor` checks the policy. |
| Reachable, but not in the Rex sidebar | It was added with `rex hosts add`, which the app ignores. Use `rex-app-host add`. |
| `rm` says it's a persistent node and the credential has auth keys only | The VM was set up with `--persistent` but the credential lacks **Devices: Core: write**. Delete the device in the console. |
| A VM was paused for a while and Rex can't reach it | Ephemeral nodes are removed after 30–60 minutes offline. The rejoin timer fixes it within about 3 minutes of the VM coming back; `sudo journalctl -u exe-rex-tailscale-rejoin` on the VM shows it. |
| `rex-app-host` reports an unexpected format | Rex changed its storage. Nothing was written. Backups are in `~/Library/Application Support/rex/app-hosts-backups/`. |
| doctor lists a `tag:exe` device with no VM | A leftover from an older or failed `rm`. Delete it under Machines in the Tailscale console. |

## Report back

Tell the human which steps ran and which were already done, whether the step 4 test passed (or exactly which check failed), any doctor warnings, and how they'll use it: `exe-rex` on its own opens the TUI (their VMs, `n` new, `d` delete, `c` settings), or the commands `exe-rex new <name>` and `exe-rex rm <name>`.
