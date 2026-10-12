package gate_test

import (
	"strings"
	"testing"

	"lintel/internal/gate"
	"lintel/internal/model"
	"lintel/internal/store"
)

func decision(kind string, paths, packages []string, status, id string) store.Decision {
	if status == "" {
		status = "approved"
	}
	if id == "" {
		id = "d1"
	}
	return store.Decision{
		ID: id, Kind: kind, Status: status,
		Proposal: store.Proposal{Scope: store.Scope{Paths: paths, Packages: packages}},
	}
}

func check(t *testing.T, root string, action model.Action, decisions []store.Decision, open *store.Decision) model.GateResult {
	t.Helper()
	return gate.Evaluate(root, action, decisions, open)
}

func TestReadsAndLintelWritesAreAllowedWhileShut(t *testing.T) {
	root := t.TempDir()
	if !check(t, root, model.Action{Kind: "read", Paths: []string{"README.md"}}, nil, nil).Allow {
		t.Fatal("read")
	}
	config := model.Action{Kind: "write", Paths: []string{".lintel/config.toml"}, Contents: map[string]string{".lintel/config.toml": "[reviewer]\n"}}
	if !check(t, root, config, nil, nil).Allow {
		t.Fatal("lintel config")
	}
	for _, command := range []string{"ls -la", "git status", "lintel propose --kind kickoff", "ls 2>/dev/null", "cd src && rg '=>' .", "git diff 2>&1 | tail"} {
		if !check(t, root, model.Action{Kind: "shell", Command: command}, nil, nil).Allow {
			t.Fatal(command)
		}
	}
}

func TestProjectWritesAreBlockedWithoutKickoff(t *testing.T) {
	root := t.TempDir()
	blocked := check(t, root, model.Action{Kind: "write", Paths: []string{"src/main.py"}, Contents: map[string]string{"src/main.py": "print(1)\n"}}, nil, nil)
	if blocked.Allow || !strings.Contains(blocked.AgentMessage, "kickoff") || !strings.Contains(blocked.AgentMessage, "lintel propose") {
		t.Fatalf("%+v", blocked)
	}
	if check(t, root, model.Action{Kind: "shell", Command: "npm create vite"}, nil, nil).Allow {
		t.Fatal("scaffold")
	}
	if check(t, root, model.Action{Kind: "shell", Command: "mkdir src"}, nil, nil).Allow {
		t.Fatal("mkdir")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"/etc/passwd"}}, nil, nil).Allow {
		t.Fatal("outside write")
	}
}

func TestApprovedKickoffAllowsScopedSource(t *testing.T) {
	root := t.TempDir()
	kickoff := decision("kickoff", []string{"**"}, []string{"react", "next"}, "", "")
	source := "import React from 'react';\nimport fs from 'fs';\n"
	if !check(t, root, model.Action{Kind: "write", Paths: []string{"src/App.tsx"}, Contents: map[string]string{"src/App.tsx": source}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("source")
	}
	manifest := "{\"name\":\"app\",\"dependencies\":{\"react\":\"1\",\"next\":\"1\"}}\n"
	if !check(t, root, model.Action{Kind: "write", Paths: []string{"package.json"}, Contents: map[string]string{"package.json": manifest}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("manifest")
	}
	if !check(t, root, model.Action{Kind: "shell", Command: "npm install"}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("npm install")
	}
	if !check(t, root, model.Action{Kind: "shell", Command: "npm install react next"}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("npm install named")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"package-lock.json"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("lockfile")
	}
}

func TestNewPackagesAndSensitivePathsNeedADecision(t *testing.T) {
	root := t.TempDir()
	kickoff := decision("kickoff", []string{"**"}, []string{"react"}, "", "")
	imported := check(t, root, model.Action{Kind: "write", Paths: []string{"src/App.tsx"}, Contents: map[string]string{"src/App.tsx": "import zod from 'zod';\n"}}, []store.Decision{kickoff}, nil)
	if imported.Allow || !strings.Contains(imported.AgentMessage, "zod") {
		t.Fatal(imported.AgentMessage)
	}
	manifest := check(t, root, model.Action{Kind: "write", Paths: []string{"package.json"}, Contents: map[string]string{"package.json": `{"dependencies":{"react":"1","zod":"1"}}`}}, []store.Decision{kickoff}, nil)
	if manifest.Allow {
		t.Fatal("manifest")
	}
	if check(t, root, model.Action{Kind: "shell", Command: "npm install zod"}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("install zod")
	}
	if check(t, root, model.Action{Kind: "shell", Command: "echo x > Dockerfile"}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("redirect")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"Dockerfile"}, Contents: map[string]string{"Dockerfile": "FROM node\n"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("dockerfile")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"prisma/schema.prisma"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("schema")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{".github/workflows/ci.yml"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("ci")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"next.config.ts"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("framework")
	}
	change := decision("change", []string{"Dockerfile", "prisma/schema.prisma", "next.config.*"}, []string{"zod"}, "", "d2")
	both := []store.Decision{kickoff, change}
	if !check(t, root, model.Action{Kind: "write", Paths: []string{"Dockerfile"}, Contents: map[string]string{"Dockerfile": "FROM node\n"}}, both, nil).Allow {
		t.Fatal("approved dockerfile")
	}
	if !check(t, root, model.Action{Kind: "write", Paths: []string{"src/App.tsx"}, Contents: map[string]string{"src/App.tsx": "import zod from 'zod';\n"}}, both, nil).Allow {
		t.Fatal("approved import")
	}
	if !check(t, root, model.Action{Kind: "shell", Command: "pnpm add zod"}, both, nil).Allow {
		t.Fatal("pnpm")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"migrations/001.sql"}}, both, nil).Allow {
		t.Fatal("migration")
	}
}

func TestPythonAndCargoManifests(t *testing.T) {
	root := t.TempDir()
	kickoff := decision("kickoff", []string{"**"}, []string{"os", "serde"}, "", "")
	if !check(t, root, model.Action{Kind: "write", Paths: []string{"src/app.py"}, Contents: map[string]string{"src/app.py": "import os\nprint(1)\n"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("stdlib")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{"src/app.py"}, Contents: map[string]string{"src/app.py": "import requests\n"}}, []store.Decision{kickoff}, nil).Allow {
		t.Fatal("requests")
	}
	cargo := "[package]\nname = \"app\"\n\n[dependencies]\nserde = \"1\"\ntokio = \"1\"\n"
	blocked := check(t, root, model.Action{Kind: "write", Paths: []string{"Cargo.toml"}, Contents: map[string]string{"Cargo.toml": cargo}}, []store.Decision{kickoff}, nil)
	if blocked.Allow || !strings.Contains(blocked.AgentMessage, "tokio") {
		t.Fatal(blocked.AgentMessage)
	}
}

func TestOpenAcceptTellsTheHumanToApprove(t *testing.T) {
	root := t.TempDir()
	open := &store.Decision{
		ID: "2026-09-27-stack", Status: "reviewed",
		Jev: &store.Jev{Verdict: "accept", Summary: "Ready for you."},
	}
	result := check(t, root, model.Action{Kind: "write", Paths: []string{"src/a.py"}}, nil, open)
	if result.Allow || !strings.Contains(result.AgentMessage, "lintel wait 2026-09-27-stack") || !strings.Contains(result.UserMessage, "Jev would do this") {
		t.Fatal(result.AgentMessage, result.UserMessage)
	}
}

func TestJevDisagreementWaitsForTheHuman(t *testing.T) {
	root := t.TempDir()
	open := &store.Decision{
		ID: "stack", Status: "reviewed",
		Proposal: store.Proposal{Choice: "Mongo for payments"},
		Jev:      &store.Jev{Verdict: "block", Summary: "Poor fit.", Concerns: []string{"A serious risk is missing."}},
	}
	result := check(t, root, model.Action{Kind: "write", Paths: []string{"src/a.py"}}, nil, open)
	if result.Allow || !strings.Contains(result.UserMessage, "Jev would not do this: Mongo for payments.") || !strings.Contains(result.UserMessage, "HUMAN_OVERRIDE") || !strings.Contains(result.AgentMessage, "lintel wait stack") {
		t.Fatal(result.UserMessage, result.AgentMessage)
	}
}

func TestAgentCannotWriteGateStateOrHooks(t *testing.T) {
	root := t.TempDir()
	kickoff := decision("kickoff", []string{"**"}, nil, "", "")
	for _, target := range []string{".lintel/decisions/a.json", ".claude/settings.json", ".claude/settings.local.json", ".codex/hooks.json", ".cursor/hooks.json", ".gemini/settings.json"} {
		result := check(t, root, model.Action{Kind: "write", Paths: []string{target}, Contents: map[string]string{target: "{}"}}, []store.Decision{kickoff}, nil)
		if result.Allow {
			t.Fatal(target)
		}
	}
	redirect := check(t, root, model.Action{Kind: "shell", Command: `echo '{"status":"authorized"}' > .lintel/decisions/a.json`}, []store.Decision{kickoff}, nil)
	if redirect.Allow || !strings.Contains(redirect.AgentMessage, "decision record") {
		t.Fatal(redirect.AgentMessage)
	}
}

func TestHarnessScratchOutsideTheProjectIsAllowed(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir()+"/tmp")
	plan := home + "/.claude/plans/plan.md"
	if !check(t, root, model.Action{Kind: "write", Paths: []string{plan}}, nil, nil).Allow {
		t.Fatal("plan")
	}
	if check(t, root, model.Action{Kind: "write", Paths: []string{home + "/.claude/settings.json"}}, nil, nil).Allow {
		t.Fatal("user settings")
	}
	if !check(t, root, model.Action{Kind: "shell", Command: "ls > ~/.claude/plans/list.txt"}, nil, nil).Allow {
		t.Fatal("redirect to plan dir")
	}
}
