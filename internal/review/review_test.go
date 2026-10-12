package review_test

import (
	"os"
	"strings"
	"testing"

	"lintel/internal/review"
	"lintel/internal/store"
)

func TestMain(m *testing.M) {
	review.APIKey = func() (string, string) {
		return strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")), ""
	}
	os.Exit(m.Run())
}

func decision() store.Decision {
	return store.Decision{
		Kind: "kickoff", Title: "App stack", Question: "What stack?", Context: "Tasks",
		Proposal: store.Proposal{
			Choice: "Next.js", Alternatives: []string{"Django"}, Why: "UI stays in one app.",
			RejectedBecause: "Two styles of UI.", Risks: []string{"upgrades"},
			Scope: store.Scope{Paths: []string{"**"}, Packages: []string{"next"}},
		},
	}
}

func config() map[string]string { return map[string]string{"model": "jev-latest"} }

func jevResponse(choice string, nouls map[string]any) map[string]any {
	answers := map[string]any{
		"verdict": map[string]any{
			"type": "choice", "choice": choice, "confidence": 0.84,
			"probabilities": map[string]any{"accept": 0.1, "revise": 0.84, "block": 0.06},
		},
	}
	for key, value := range nouls {
		answers[key] = map[string]any{"type": "noul", "noul": value}
	}
	return map[string]any{"model": "jev-latest", "answers": answers}
}

func TestMissingKeyStaysShut(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	result := review.Proposal(decision(), config(), nil, nil)
	if result.Verdict != "revise" || !strings.Contains(result.Concerns[0], "lintel login") {
		t.Fatal(result)
	}
}

func TestRequestUsesTheTypesafeEndpoint(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	seen := map[string]any{}
	result := review.Proposal(decision(), map[string]string{"model": "claude-sonnet-4-5"}, nil, func(url string, headers map[string]string, payload map[string]any) (map[string]any, error) {
		seen["url"] = url
		seen["headers"] = headers
		seen["payload"] = payload
		return jevResponse("block", map[string]any{"high_lock_in": 0.91}), nil
	})
	if result.Verdict != "block" || result.Valid || result.Evidence != "unsupported" {
		t.Fatalf("%+v", result)
	}
	if seen["url"] != review.Endpoint {
		t.Fatal(seen["url"])
	}
	headers := seen["headers"].(map[string]string)
	if headers["authorization"] != "Bearer ts_test" {
		t.Fatal(headers)
	}
	payload := seen["payload"].(map[string]any)
	if payload["model"] != "jev-latest" {
		t.Fatal(payload["model"])
	}
	state := payload["state"].(map[string]any)
	if state["proposed_change"] != "Next.js" {
		t.Fatal(state["proposed_change"])
	}
	questions := payload["questions"].(map[string]any)
	if questions["verdict"].(map[string]any)["type"] != "choice" || questions["affected"].(map[string]any)["type"] != "choice" {
		t.Fatal(questions)
	}
}

func TestProbabilityDoesNotChangeTheVerdict(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	result := review.Proposal(decision(), config(), nil, func(string, map[string]string, map[string]any) (map[string]any, error) {
		return jevResponse("accept", nil), nil
	})
	if result.Verdict != "accept" || !result.Valid || result.Probabilities["accept"] != 0.1 || strings.Contains(result.Summary, "0.41") {
		t.Fatalf("%+v", result)
	}
}

func TestBlockWithoutAnInvariantIsRejected(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	result := review.Proposal(decision(), config(), nil, func(string, map[string]string, map[string]any) (map[string]any, error) {
		response := jevResponse("block", nil)
		response["answers"].(map[string]any)["affected"] = map[string]any{"type": "choice", "choice": "none"}
		return response, nil
	})
	if result.Verdict != "block" || result.Valid || result.Evidence != "unsupported" || result.Conflict != "" {
		t.Fatalf("%+v", result)
	}
}

func TestBlockNamesTheInvariant(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	active := store.Decision{
		ID: "auth-decision", Status: "authorized", Kind: "change",
		Proposal: store.Proposal{
			Invariants: []store.Named{{ID: "auth.membership", Statement: "A request must be authorized by current organization membership."}},
			Scope:      store.Scope{Paths: []string{"src/auth/**"}, Components: []string{"getProject"}},
		},
	}
	proposal := store.Decision{
		ID: "new-decision", Kind: "change", Title: "Drop the membership check", Question: "Can the loader trust the id?",
		Proposal: store.Proposal{
			Choice: "Load by id only", Intent: "Skip the membership query", ProposedChange: "Load the project by id",
			Scope: store.Scope{Paths: []string{"src/auth/loader.ts"}, Components: []string{"getProject"}},
		},
	}
	result := review.Proposal(proposal, config(), []store.Decision{active}, func(_ string, _ map[string]string, payload map[string]any) (map[string]any, error) {
		criteria := payload["questions"].(map[string]any)["affected"].(map[string]any)["criteria"].(map[string]any)
		if !strings.HasPrefix(criteria["auth.membership"].(string), "A request ") {
			t.Fatal(criteria)
		}
		if payload["state"].(map[string]any)["scope_match"] != "path" {
			t.Fatal(payload["state"])
		}
		return map[string]any{"answers": map[string]any{
			"verdict":  map[string]any{"choice": "block", "confidence": 0.2, "probabilities": map[string]any{"accept": 0.01, "revise": 0.04, "block": 0.95}},
			"affected": map[string]any{"choice": "auth.membership"},
		}}, nil
	})
	if result.Verdict != "block" || !result.Valid || result.Code != "JEV_BLOCK" || result.AffectedInvariant != "auth.membership" {
		t.Fatalf("%+v", result)
	}
	if !strings.Contains(result.Conflict, "auth.membership") || result.RequiredChange == "" || result.AcceptanceCriteria == "" || result.Evidence != "direct" || result.Probabilities["block"] != 0.95 {
		t.Fatalf("%+v", result)
	}
}

func TestPathOverlapAndWeakText(t *testing.T) {
	auth := store.Decision{
		ID: "auth", Status: "authorized",
		Proposal: store.Proposal{
			Invariants: []store.Named{{ID: "auth.membership", Statement: "Membership is the authorization boundary."}},
			Scope:      store.Scope{Paths: []string{"src/auth/**"}, Components: []string{"getProject"}},
		},
	}
	billing := store.Decision{
		ID: "billing", Status: "authorized",
		Proposal: store.Proposal{
			Invariants: []store.Named{{ID: "billing.owner", Statement: "Membership decides who can see an invoice."}},
			Scope:      store.Scope{Paths: []string{"src/billing/**"}, Components: []string{"invoice"}},
		},
	}
	proposal := store.Decision{ID: "proposal", Proposal: store.Proposal{Choice: "Keep the membership check", Scope: store.Scope{Paths: []string{"src/auth/loader.ts"}}}}
	active, match := review.SelectActive(proposal, []store.Decision{auth, billing})
	if match != "path" || len(active) != 1 || active[0].ID != "auth" {
		t.Fatalf("%s %+v", match, active)
	}
	notes := store.Decision{ID: "notes", Proposal: store.Proposal{Choice: "Membership authorization stays in the loader", Scope: store.Scope{Paths: []string{"docs/notes.md"}}}}
	active, match = review.SelectActive(notes, []store.Decision{auth, billing})
	if match != "weak" || len(active) != 2 || active[0].ID != "auth" || active[1].ID != "billing" {
		t.Fatalf("%s %+v", match, active)
	}
}

func TestInvalidAnswerStaysShut(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	result := review.Proposal(decision(), config(), nil, func(string, map[string]string, map[string]any) (map[string]any, error) {
		return map[string]any{"answers": map[string]any{"verdict": map[string]any{"choice": "maybe"}}}, nil
	})
	if result.Valid || result.Verdict != "revise" || !strings.Contains(result.Summary, "invalid verdict") {
		t.Fatal(result.Summary)
	}
	if len(result.Concerns) != 1 || result.Concerns[0] != "Fix the TypeSafe configuration and run lintel review." {
		t.Fatal(result.Concerns)
	}
}

func TestReviseWithoutAnInvariantStaysOnTheCard(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	proposal := store.Decision{
		Kind: "change", Title: "Sort tasks", Question: "Sort them?",
		Proposal: store.Proposal{
			Choice: "sort tasks differently", Intent: "Change how tasks are sorted.", ProposedChange: "sort tasks differently",
			Scope: store.Scope{Paths: []string{"src/tasks.py"}},
		},
	}
	result := review.Proposal(proposal, config(), nil, func(string, map[string]string, map[string]any) (map[string]any, error) {
		response := jevResponse("revise", nil)
		response["answers"].(map[string]any)["affected"] = map[string]any{"type": "choice", "choice": "none"}
		return response, nil
	})
	if result.Verdict != "revise" || !result.Valid || !strings.Contains(result.Summary, "acceptance criteria") || !strings.Contains(result.RequiredChange, "acceptance criteria") || result.AffectedInvariant != "" {
		t.Fatalf("%+v", result)
	}
}

func TestSupersedingAKickoffThatCarriesOtherRulesIsRevise(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	kickoff := store.Decision{
		ID: "2026-09-27-python-task-tracker", Status: "authorized", Kind: "kickoff",
		Proposal: store.Proposal{
			Invariants: []store.Named{
				{ID: "tenant.isolation", Statement: "Queries stay in the caller's organization."},
				{ID: "auth.membership", Statement: "Membership comes from the session."},
				{ID: "money.integer", Statement: "Money is integer cents."},
			},
			Scope: store.Scope{Paths: []string{"src/**"}},
		},
	}
	proposal := store.Decision{
		ID: "tokens", Kind: "change", Title: "Signed org tokens", Question: "Replace membership?",
		Proposal: store.Proposal{
			Choice: "signed org tokens replace membership checks", Intent: "Retire membership checks",
			ProposedChange:     "signed org tokens replace membership checks",
			Scope:              store.Scope{Paths: []string{"src/auth.py"}},
			Supersedes:         []string{"2026-09-27-python-task-tracker"},
			Invariants:         []store.Named{{ID: "auth.token", Statement: "A request is authorized only by a valid signed organization token."}},
			AcceptanceCriteria: []string{"current_org validates the token"},
		},
	}
	result := review.Proposal(proposal, config(), []store.Decision{kickoff}, func(string, map[string]string, map[string]any) (map[string]any, error) {
		return map[string]any{"answers": map[string]any{
			"verdict":  map[string]any{"choice": "block", "probabilities": map[string]any{"block": 0.9}},
			"affected": map[string]any{"choice": "auth.membership"},
		}}, nil
	})
	if result.Verdict != "revise" || !result.Valid || result.Code == "JEV_BLOCK" {
		t.Fatalf("%+v", result)
	}
	if !strings.Contains(result.Summary, "tenant.isolation") || !strings.Contains(result.Summary, "money.integer") || !strings.Contains(result.Summary, "shuts the gate") {
		t.Fatal(result.Summary)
	}
}

func TestSupersedingOnlyTheContradictedDecisionIsAccept(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "ts_test")
	auth := store.Decision{
		ID: "auth-decision", Status: "authorized", Kind: "change",
		Proposal: store.Proposal{
			Invariants: []store.Named{{ID: "auth.membership", Statement: "Membership comes from the session."}},
			Scope:      store.Scope{Paths: []string{"src/auth/**"}},
		},
	}
	proposal := store.Decision{
		ID: "tokens", Kind: "change", Title: "Signed org tokens", Question: "Replace membership?",
		Proposal: store.Proposal{
			Choice: "signed org tokens", Intent: "Retire membership checks", ProposedChange: "signed org tokens",
			Scope: store.Scope{Paths: []string{"src/auth/loader.ts"}}, Supersedes: []string{"auth-decision"},
			Invariants:         []store.Named{{ID: "auth.token", Statement: "A request is authorized only by a valid signed organization token."}},
			AcceptanceCriteria: []string{"current_org validates the token"},
		},
	}
	result := review.Proposal(proposal, config(), []store.Decision{auth}, func(string, map[string]string, map[string]any) (map[string]any, error) {
		return map[string]any{"answers": map[string]any{
			"verdict":  map[string]any{"choice": "block", "probabilities": map[string]any{"block": 0.8}},
			"affected": map[string]any{"choice": "auth.membership"},
		}}, nil
	})
	if result.Verdict != "accept" || !result.Valid || !strings.Contains(result.Summary, "supersedes") || result.Code != "" {
		t.Fatalf("%+v", result)
	}
}
