# Lintel

Lintel is a change gate for AI coding agents. The agent proposes a choice, TypeSafe's Jev reviews it, and a person approves it before the agent writes code. The command is `lintel`.

Jev advises and you decide. After a proposal, the agent runs `lintel wait` in the same turn, and you approve or reject without leaving your coding tool:

| Tool | Where you decide |
|---|---|
| Claude Code, Gemini CLI | The tool's own permission prompt, showing Jev's card. Allow approves. |
| Cursor | Cursor's prompt on `lintel approve <id>`. Run approves. |
| Codex | A window titled Jev decision (Codex hooks cannot ask yet), plus a desktop notification. |

The agent cannot approve itself. It cannot run `lintel approve` in Codex, and it cannot edit `.lintel/` or the hook config anywhere. A stop hook keeps the agent in the turn while a decision waits, and a session-start hook tells every new session the gate state.

## Install

Build one file for your machine:

```bash
go build -o lintel ./cmd/lintel
```

Put that file on `PATH`. The same source cross-compiles without a C toolchain:

```bash
GOOS=linux GOARCH=amd64 go build -o lintel-linux ./cmd/lintel
GOOS=darwin GOARCH=amd64 go build -o lintel-macos ./cmd/lintel
GOOS=windows GOARCH=amd64 go build -o lintel.exe ./cmd/lintel
```

Then, in a project:

```bash
lintel init
```

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
