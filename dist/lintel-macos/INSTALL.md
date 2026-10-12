# Lintel for macOS: test build

Lintel is a change gate for AI coding agents (Claude Code, Codex, Cursor, Gemini CLI). Before the agent adds a package, makes an architecture choice, or touches CI, Docker, schema, or framework config, it has to propose the choice. TypeSafe's Jev reviews the proposal and **you** approve or reject it, usually right inside your coding tool.

This is an unsigned test build. It needs macOS 12 (Monterey) or later.

## 1. Pick the right binary

```bash
uname -m
```

- `arm64`: Apple Silicon (M1–M4), so use `lintel-arm64`
- `x86_64`: Intel Mac, so use `lintel-amd64`

## 2. Install it

From the folder you unzipped (swap in `lintel-amd64` on Intel):

```bash
mkdir -p ~/.local/bin
cp lintel-arm64 ~/.local/bin/lintel
chmod +x ~/.local/bin/lintel
xattr -d com.apple.quarantine ~/.local/bin/lintel 2>/dev/null || true
```

The `xattr` line removes the "downloaded from the internet" flag. Without it, macOS will refuse to run the unsigned binary.

Make sure `~/.local/bin` is on your PATH. If `which lintel` prints nothing, run this, then open a new terminal:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

Check that it runs:

```bash
lintel
# usage: lintel <init|login|logout|ask|propose|...>
```

If macOS still says it "cannot be opened because the developer cannot be verified", open **System Settings → Privacy & Security** and click **Allow Anyway**, then run it again.

## 3. Add your TypeSafe key

Create a key at https://console.typesafe.ai/keys, then:

```bash
lintel login
```

Paste the key when asked. It is not shown on screen and is stored in your macOS Keychain, not in any project. Without a key the gate stays shut and nothing gets approved.

## 4. Turn it on in a project

Use a throwaway or git-committed project the first time:

```bash
cd path/to/your/project
lintel init
```

This creates `.lintel/`, `LINTEL.md` (the rules the agent follows), pointers in `CLAUDE.md` / `AGENTS.md` / `GEMINI.md`, and hook config for each tool. When `which lintel` finds it, the hooks call plain `lintel hook <tool>` and the same config works for teammates on Linux and Windows. If `lintel` is not on PATH, the hooks point at its full path instead, so don't move the binary afterwards; if you do, run `lintel init` again.

If the project already has hook config from another machine (for example `/home/...` paths from Linux), run `lintel init` once to replace it.

Then, per tool:

- **Claude Code:** start a new session in the project. If it asks you to review the new hooks, accept them.
- **Codex:** run `/hooks` and trust the Lintel hooks. Codex skips project hooks until you do.
- **Cursor / Gemini CLI:** nothing extra.

## 5. Try it

Ask your agent for something that needs a decision, for example:

> Add a small "tasks" feature to this project. Pick whatever library you think fits.

What should happen:

1. The agent looks around, may run `lintel ask` to have Jev pick between libraries, then runs `lintel propose`.
2. You're asked to decide:
   - **Claude Code / Gemini:** a normal permission prompt showing a short card (what's proposed, why, what it touches, and Jev's view). **Allow** approves; **Deny** rejects (add a note to say why).
   - **Cursor:** Cursor's prompt on `lintel approve …`. **Run** approves.
   - **Codex:** a macOS dialog titled **Jev decision** with **Approve** / **Reject** / **Not yet**, plus a notification.
3. Once approved, the agent implements only what was approved. If it tries to add something else (a new package, CI, Docker…), it gets blocked and has to propose again.

Useful commands in a terminal:

```bash
lintel status     # is the gate open, and what is waiting
lintel show       # full details of the open decision
lintel approve    # approve from the terminal instead
lintel reject --note "Use Postgres instead."
```

## Feedback wanted

- Did the approval prompt or window appear every time it should have?
- Was the card easy to read and enough to decide on?
- Did the agent get blocked on something harmless? Paste the exact message.
- Which tool and version were you using (`claude --version`, `codex --version`, …)?

## Remove it

```bash
lintel logout          # deletes the key from your Keychain
rm ~/.local/bin/lintel
```

In a project, delete `.lintel/`, `LINTEL.md`, and the Lintel entries in `.claude/settings.json`, `.codex/hooks.json`, `.cursor/hooks.json`, and `.gemini/settings.json`.
