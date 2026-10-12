# Lintel

Lintel is a change gate for AI coding agents. The agent proposes a choice, TypeSafe's Jev reviews it, and a person approves it before the agent writes code. The command is `lintel`.

Jev advises and you decide. After a proposal, the agent runs `lintel wait` in the same turn, and you approve or reject without leaving your coding tool:

| Tool | Where you decide |
|---|---|
| Claude Code, Gemini CLI | The tool's own permission prompt, showing Jev's card. Allow approves. |
| Cursor | Cursor's prompt on `lintel approve <id>`. Run approves. |
| Codex | A window titled Jev decision (Codex hooks cannot ask yet), plus a desktop notification. |

The agent cannot approve itself. It cannot run `lintel approve` in Codex, and it cannot edit `.lintel/` or the hook config anywhere. A stop hook keeps the agent in the turn while a decision waits, and a session-start hook tells every new session the gate state.

## Requirements

- Go 1.22 or later to build (the binary itself needs no runtime and only uses the Go standard library)
- A TypeSafe API key for Jev reviews, from [console.typesafe.ai/keys](https://console.typesafe.ai/keys)
- For the decision window: zenity on Linux. macOS and Windows have what they need built in.
- For key storage on Linux: `secret-tool` (libsecret)

## Install

Clone the repo and run the install script. It builds `lintel` from the source you just cloned and copies it into your user bin directory. It needs no sudo or admin rights and downloads nothing except what Go itself fetches. Read the script first if you like; it is about 40 lines.

**Linux and macOS** (installs to `~/.local/bin`):

```bash
git clone https://github.com/Capzay/Lintel.git
cd Lintel
./install.sh
```

**Windows** (PowerShell, installs to `%LOCALAPPDATA%\Programs\lintel` and adds that folder to your user PATH):

```powershell
git clone https://github.com/Capzay/Lintel.git
cd Lintel
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

Set `LINTEL_BIN_DIR` to install somewhere else. Open a new terminal, then check that `lintel` runs.

To build it by hand, run:

```bash
go build -o lintel ./cmd/lintel
```

The same source cross-compiles without a C toolchain:

```bash
GOOS=linux GOARCH=amd64 go build -o lintel-linux ./cmd/lintel
GOOS=darwin GOARCH=arm64 go build -o lintel-macos ./cmd/lintel
GOOS=windows GOARCH=amd64 go build -o lintel.exe ./cmd/lintel
```

## Quick start

```bash
lintel login          # store your TypeSafe key in the system keyring
cd path/to/project
lintel init           # add the gate and hooks to this project
```

Then start a new session in your coding tool. In Codex, run `/hooks` and trust the Lintel hooks.

`lintel init` writes `.lintel/`, hook config for Cursor, Claude, Codex, and Gemini, and `LINTEL.md`. When the binary you ran is the `lintel` on `PATH`, the hooks call plain `lintel hook <tool>`, so the committed config works on Linux, macOS, and Windows alike. A binary that is not on `PATH` is written as its absolute path instead (forward slashes and double quotes on Windows), which only works on that machine. Re-running `lintel init` replaces older Lintel or Jev hook entries and keeps everything else. Decisions from the Python prototype's `.jev/` are imported the first time. Codex will not run project hooks until you trust them with `/hooks`; trust them again after an update.

Create a key at [console.typesafe.ai/keys](https://console.typesafe.ai/keys), then store it in the system keyring:

```bash
lintel login
```

The prompt does not echo the key. Linux uses the Secret Service (`secret-tool`), macOS uses Keychain, and Windows uses Credential Manager. `lintel logout` removes it. The key is not written into the project or the shell profile. The model in `.lintel/config.toml` defaults to `jev-latest`. If no key is stored, the gate stays shut.

On Linux the decision window uses zenity. On macOS it uses `osascript`. On Windows it uses PowerShell. It runs detached from the agent's shell (on Linux through `systemd-run --user`, which also escapes the Codex sandbox), so a shell timeout does not close it. Approve and Reject are the only choices. Not yet, Enter, and closing the window leave the decision unchanged, and `lintel wait` reopens it a few minutes later. If no window can be shown, `lintel wait` says so and you can run `lintel approve` in a terminal.

## Ask Jev

When the agent is choosing a package or an approach, it asks Jev first. It lists the candidates, and Jev picks the one that fits this codebase: its declared dependencies, file tree, the files named with `--path`, and the approved decisions and invariants.

```bash
lintel ask --question "Which Markdown renderer?" \
  --option "goldmark: CommonMark, pure Go" --option "blackfriday: older, not CommonMark" \
  --path internal/docs/render.go
lintel propose --from-advice <advice-id> --kind change --path "docs/**" --package github.com/yuin/goldmark
```

Advice is saved in `.lintel/advice/`. It is only advice: the proposal still goes through Jev's review and your approval.

## Commands

```bash
lintel ask --question "What stack?" --option "next: Next.js" --option "remix: Remix"
lintel propose --kind kickoff --title "App stack" --question "What stack?" \
  --choice "Next.js" --path "**" --package next --package react
lintel wait
lintel show
lintel approve
lintel status
lintel reject --note "Pick a smaller stack."
lintel review
```

A kickoff must be approved before a `change` proposal. One proposal stays open at a time. After kickoff, ordinary source inside the approved paths can be edited. New packages, schema, CI, Docker, and framework config need an approved decision that names them. A path of `**` does not unlock those sensitive files. Lockfiles stay blocked; install approved packages with the package manager.

`lintel check` prints whether one read, write, or shell action is allowed. The tool hooks call the same policy.

## Security

- Your API key goes into the OS keyring (Secret Service, Keychain, or Credential Manager). It is never written to the project, to `.lintel/`, or to your shell profile. `TYPESAFE_API_KEY` works as a fallback for CI.
- `lintel propose` sends Jev the proposal and your approved decisions. `lintel ask` also sends your declared dependencies, part of the file tree, and the contents of any files you name with `--path`. Don't name files that hold secrets.
- Decision records in `.lintel/decisions/` are meant to be committed. They contain the proposal, Jev's verdict, and the human decision, and no credentials.

## Uninstall

```bash
lintel logout                 # remove the key from the keyring
rm ~/.local/bin/lintel        # Windows: delete %LOCALAPPDATA%\Programs\lintel
```

In a project, delete `.lintel/` and `LINTEL.md`, plus the Lintel entries in `.claude/settings.json`, `.codex/hooks.json`, `.cursor/hooks.json`, and `.gemini/settings.json`.

## Development

```bash
go test ./...
```
