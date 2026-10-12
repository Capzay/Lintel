# Lintel for Windows: test build

Lintel is a change gate for AI coding agents (Claude Code, Codex, Cursor, Gemini CLI). Before the agent adds a package, makes an architecture choice, or touches CI, Docker, schema, or framework config, it has to propose the choice. TypeSafe's Jev reviews the proposal and **you** approve or reject it, usually right inside your coding tool.

This is an unsigned test build. It needs Windows 10 or 11 and Windows PowerShell (built in). Run the commands below in **PowerShell**, not Command Prompt.

## 1. Pick the right binary

```powershell
$env:PROCESSOR_ARCHITECTURE
```

- `AMD64`: most PCs, so use `lintel-amd64.exe`
- `ARM64`: Snapdragon / Surface Pro X and other Arm PCs, so use `lintel-arm64.exe`

## 2. Install it

From the folder you unzipped (swap in `lintel-arm64.exe` on Arm):

```powershell
$bin = "$env:LOCALAPPDATA\Programs\lintel"
New-Item -ItemType Directory -Force $bin | Out-Null
Copy-Item .\lintel-amd64.exe "$bin\lintel.exe"
Unblock-File "$bin\lintel.exe"
```

`Unblock-File` removes the "downloaded from the internet" flag, so SmartScreen won't stop the unsigned binary. If SmartScreen still shows "Windows protected your PC", click **More info → Run anyway**.

Add that folder to your user PATH (once), then **open a new terminal**:

```powershell
$path = [Environment]::GetEnvironmentVariable('Path', 'User')
[Environment]::SetEnvironmentVariable('Path', "$path;$bin", 'User')
```

Check that it runs:

```powershell
lintel
# usage: lintel <init|login|logout|ask|propose|...>
```

## 3. Add your TypeSafe key

Create a key at https://console.typesafe.ai/keys, then:

```powershell
lintel login
```

Paste the key when asked. It is not shown on screen and is stored in Windows Credential Manager (as `lintel/typesafe`), not in any project. Without a key the gate stays shut and nothing gets approved.

On a work PC where Group Policy forces a PowerShell execution policy, `lintel login` can still be blocked; ask IT, or paste the error into your feedback.

## 4. Turn it on in a project

Use a throwaway or git-committed project the first time:

```powershell
cd path\to\your\project
lintel init
```

This creates `.lintel\`, `LINTEL.md` (the rules the agent follows), pointers in `CLAUDE.md` / `AGENTS.md` / `GEMINI.md`, and hook config for each tool. Run it from a new terminal so `lintel` is on PATH: the hooks then call plain `lintel hook <tool>` and the same config works for teammates on Linux and macOS. If `lintel.exe` is not on PATH, the hooks point at its full path instead, so don't move it afterwards; if you do, run `lintel init` again.

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
   - **Codex:** a window titled **Jev decision** with **Approve** / **Reject** / **Not yet**.
3. Once approved, the agent implements only what was approved. If it tries to add something else (a new package, CI, Docker…), it gets blocked and has to propose again.

Useful commands in a terminal:

```powershell
lintel status     # is the gate open, and what is waiting
lintel show       # full details of the open decision
lintel approve    # approve from the terminal instead
lintel reject --note "Use Postgres instead."
```

## Feedback wanted

- Did the approval prompt or window appear every time it should have?
- Did the hooks run in each tool? If a tool reports a hook error, paste it exactly.
- Was the card easy to read and enough to decide on?
- Did the agent get blocked on something harmless? Paste the exact message.
- Which tool and version were you using (`claude --version`, `codex --version`, …)?

## Remove it

```powershell
lintel logout          # deletes the key from Credential Manager
Remove-Item -Recurse "$env:LOCALAPPDATA\Programs\lintel"
```

Then remove `%LOCALAPPDATA%\Programs\lintel` from your user PATH (Settings → System → About → Advanced system settings → Environment Variables).

In a project, delete `.lintel\`, `LINTEL.md`, and the Lintel entries in `.claude\settings.json`, `.codex\hooks.json`, `.cursor\hooks.json`, and `.gemini\settings.json`.
