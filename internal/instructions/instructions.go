// Package instructions is the text installed so every coding agent follows the same gate.
package instructions

const Markdown = `# Lintel

Every coding agent follows the same three steps before it writes project code:

1. The agent proposes the choice.
2. TypeSafe's Jev reviews that choice. The coding agent does not grade its own decision.
3. A human approves it. The gate stays shut until this happens.

Jev advises. The human decides. After ` + "`lintel propose`" + `, run ` + "`lintel wait`" + ` in the same turn. Do not end the turn to ask in prose, and do not approve on the human's behalf.

How the human is asked depends on the tool, and the hooks handle it:

- Claude Code and Gemini: ` + "`lintel wait`" + ` becomes the tool's own permission prompt, showing Jev's card. Allow approves. If they deny, run ` + "`lintel reject <id> --note \"<their words>\"`" + `, then stop.
- Cursor: run ` + "`lintel approve <id>`" + `. Cursor asks the human before it runs; that prompt is their decision. If they skip it, run ` + "`lintel reject <id> --note`" + ` with their words, then stop.
- Codex: ` + "`lintel wait`" + ` opens a window titled Jev decision and keeps printing while it waits. Keep polling it until it returns. If it says it is still waiting, run it again. Codex may not run ` + "`lintel approve`" + `.

A hook stops the turn from ending while a reviewed decision waits, and a new session is told the gate state.

When wait returns authorized, implement only that scope. When it returns rejected, stop. Do not propose a replacement unless the human asks for one. A human yes after Jev blocks is ` + "`HUMAN_OVERRIDE`" + `. Jev's verdict stays ` + "`JEV_BLOCK`" + `.

## Ask Jev first

When you are choosing between packages or approaches, ask Jev before you propose. List the real candidates; Jev picks the one that fits this codebase, its dependencies, and its approved decisions.

` + "```bash" + `
lintel ask \
  --question "Which Markdown renderer should the docs page use?" \
  --context "Server-rendered, must sanitize user input." \
  --option "goldmark: CommonMark, pure Go, extensible" \
  --option "blackfriday: older, fast, not CommonMark" \
  --path internal/docs/render.go
` + "```" + `

Then propose the recommendation with ` + "`lintel propose --from-advice <advice-id> --kind change --path ... --package ...`" + `. The choice, alternatives, and question come from the advice. You may propose a different option; say why.

## While the gate is shut

You may read files, run read-only commands (` + "`ls`" + `, ` + "`cat`" + `, ` + "`rg`" + `, ` + "`git status`" + `, ` + "`git diff`" + `), and run ` + "`lintel`" + `.

Do not create files, edit files, install packages, or scaffold a project. Hooks block those actions. Do not work around a denial with another tool, a redirect, or a different command. Never edit ` + "`.lintel/`" + ` or the hook config directly.

## Kickoff

Before the first project files, propose the stack. List ordinary source as ` + "`**`" + ` if the whole tree is in scope. List schema, CI, Docker, and framework config paths explicitly, because a catch-all path does not unlock them. List every package you will install or import.

` + "```bash" + `
lintel propose \
  --kind kickoff \
  --title "App stack" \
  --question "What stack should this app use?" \
  --context "A small web app for tracking tasks." \
  --choice "Next.js and Postgres" \
  --alternative "Remix and Postgres" \
  --alternative "Django" \
  --why "Next.js fits the UI, and Postgres keeps the data model simple." \
  --rejected-because "Django would split the app across two styles of UI code." \
  --risk "Next.js upgrades can be noisy." \
  --path "**" \
  --path "Dockerfile" \
  --package next \
  --package react \
  --package react-dom
` + "```" + `

Then run ` + "`lintel wait`" + ` in the same turn (in Cursor, ` + "`lintel approve <id>`" + `). Do not stop to ask the human in prose.

Name what must stay true with ` + "`--invariant id:must stay true`" + `. Name the current way of doing it with ` + "`--implementation id:current way`" + `. A temporary requirement uses ` + "`--constraint id:requirement|until this is done`" + `. ` + "`--supersedes <decision-id>`" + ` retires a previous implementation decision when the human authorizes the new one. ` + "`--acceptance`" + ` states how you will know the change is right.

## Later choices

After kickoff is approved, ordinary edits inside the approved paths are allowed. Propose a new ` + "`change`" + ` decision before you:

- add a dependency or import a package that was not approved
- edit schema, migrations, CI, Docker, or framework config that was not listed
- make an architecture choice the kickoff did not cover

Do not hand-edit lockfiles. After the package is on an approved decision, install it with the package manager.

One decision can be open at a time. After every proposal, run ` + "`lintel wait`" + ` in the same turn (in Cursor, ` + "`lintel approve <id>`" + `). When wait returns rejected, stop. Propose again only when the human asks for a different choice.

## Human commands

` + "```bash" + `
lintel login
lintel logout
lintel ask --question "..." --option "a: ..." --option "b: ..."
lintel status
lintel show
lintel wait
lintel approve
lintel reject --note "Use Postgres instead."
lintel review
` + "```" + `

Codex ignores project hooks until they are trusted with ` + "`/hooks`" + `.
`

const Pointer = `## Lintel

Follow [LINTEL.md](LINTEL.md) before writing any code. The gate blocks project edits until a human approves Jev's review.
`

const ConfigTOML = `# TypeSafe's Jev reviews proposals. Store the API key in the system keyring:
#   lintel login
# Create a key at https://console.typesafe.ai/keys
# Do not put the key in this file.

[reviewer]
model = "jev-latest"
`
