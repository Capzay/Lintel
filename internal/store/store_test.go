package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"lintel/internal/initcmd"
	"lintel/internal/model"
	"lintel/internal/review"
	"lintel/internal/store"
)

func accept(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
	return model.ReviewResult{Verdict: "accept", Summary: "The choice fits the feature.", Concerns: []string{}, Valid: true, Evidence: "none"}
}

func revise(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
	return model.ReviewResult{
		Verdict: "revise", Summary: "Name the database.", Concerns: []string{"stack.database"}, Valid: true,
		AffectedInvariant: "stack.database", Conflict: "The stack does not name a database.",
		RequiredChange: "Name the database.", AcceptanceCriteria: "The proposal names the database.", Evidence: "direct",
	}
}

func block(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
	return model.ReviewResult{
		Verdict: "block", Summary: "Membership is the authorization boundary.", Concerns: []string{"auth.membership"}, Valid: true,
		AffectedInvariant: "auth.membership", Conflict: "Dropping the membership check contradicts the authorization boundary.",
		RequiredChange:     "Keep the membership check, or supersede this invariant explicitly.",
		AcceptanceCriteria: "A non-member cannot read the project.", Evidence: "direct", Code: "JEV_BLOCK",
	}
}

func project(t *testing.T) store.Project {
	t.Helper()
	root := t.TempDir()
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	return store.Open(root)
}

func proposal(overrides map[string]any) map[string]any {
	fields := map[string]any{
		"kind": "kickoff", "title": "App stack", "question": "What stack should this app use?",
		"context": "A task tracker.", "choice": "Next.js and Postgres", "alternatives": []string{"Django"},
		"why": "One language for the UI.", "rejected_because": "Django splits the UI.",
		"risks": []string{"Upgrade noise"}, "paths": []string{"**", "Dockerfile"}, "packages": []string{"next", "react"},
	}
	for key, value := range overrides {
		fields[key] = value
	}
	return fields
}

func TestProposeReviewsImmediately(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(nil), accept)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != "reviewed" || decision.Jev.Verdict != "accept" || !strings.Contains(store.FormatCard(decision), "Next.js") {
		t.Fatal(store.FormatCard(decision))
	}
	status, err := store.FormatStatus(item)
	if err != nil || !strings.Contains(status, "Gate      shut") {
		t.Fatal(status, err)
	}
}

func TestApproveOpensGateAndRejectClosesProposal(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(nil), accept)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := item.Approve(decision.ID, "Ship it.")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "authorized" || approved.Human.Authorization != "authorized" {
		t.Fatal(approved.Status)
	}
	status, _ := store.FormatStatus(item)
	if !strings.Contains(status, "Gate      open") {
		t.Fatal(status)
	}
	other, err := item.Propose(proposal(map[string]any{
		"title": "Auth library", "kind": "change", "choice": "lucia", "packages": []string{"lucia"}, "paths": []string{"src/**"},
	}), revise)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := item.Reject(other.ID, "Stay with the platform session.")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != "rejected" {
		t.Fatal(rejected.Status)
	}
	open, err := item.OpenDecisions()
	if err != nil || len(open) != 0 {
		t.Fatal(open, err)
	}
}

func TestHumanYesAfterABlockIsAnOverride(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(map[string]any{"title": "Drop the membership check"}), block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store.FormatCard(decision), "Jev would not do this") || decision.Jev.Code != "JEV_BLOCK" {
		t.Fatal(store.FormatCard(decision))
	}
	overridden, err := item.Approve(decision.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Status != "overridden" || overridden.Human.Authorization != "HUMAN_OVERRIDE" || overridden.Jev.Verdict != "block" || overridden.Jev.Code != "JEV_BLOCK" {
		t.Fatalf("%+v", overridden.Human)
	}
	status, _ := store.FormatStatus(item)
	if !strings.Contains(status, "Gate      open") {
		t.Fatal(status)
	}
}

func TestReviseCanBeUpdated(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(nil), revise)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := item.Propose(proposal(map[string]any{"choice": "Django", "why": "Smaller."}), accept)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != decision.ID || updated.Proposal.Choice != "Django" || updated.Jev.Verdict != "accept" {
		t.Fatal(store.FormatCard(updated))
	}
	if !strings.Contains(store.FormatCard(updated), "Jev would do this") {
		t.Fatal("advice")
	}
}

func TestWaitingAcceptCannotBeReplaced(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(nil), accept)
	if err != nil {
		t.Fatal(err)
	}
	_, err = item.Propose(proposal(map[string]any{"title": "Other"}), accept)
	var gateErr *store.Error
	if !errors.As(err, &gateErr) {
		t.Fatal(err)
	}
	open, _ := item.OpenDecisions()
	if len(open) != 1 || open[0].ID != decision.ID {
		t.Fatal(open)
	}
}

func TestChangeRequiresKickoffAndNewKickoffSupersedes(t *testing.T) {
	item := project(t)
	_, err := item.Propose(proposal(map[string]any{"kind": "change", "title": "Add redis"}), accept)
	var gateErr *store.Error
	if !errors.As(err, &gateErr) {
		t.Fatal(err)
	}
	first, err := item.Propose(proposal(nil), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.Approve(first.ID, ""); err != nil {
		t.Fatal(err)
	}
	second, err := item.Propose(proposal(map[string]any{"title": "Replacement stack", "packages": []string{"django"}}), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.Approve(second.ID, ""); err != nil {
		t.Fatal(err)
	}
	older, err := item.Load(first.ID)
	if err != nil || older.Status != "superseded" || older.SupersededBy != second.ID {
		t.Fatal(older.Status, older.SupersededBy, err)
	}
	newer, _ := item.Load(second.ID)
	if newer.Status != "authorized" {
		t.Fatal(newer.Status)
	}
}

func TestWaitReturnsTheHumanDecision(t *testing.T) {
	item := project(t)
	decision, err := item.Propose(proposal(nil), accept)
	if err != nil {
		t.Fatal(err)
	}
	waited, err := item.WaitForHuman(decision.ID, 5*time.Second, 0, func(time.Duration) {
		_, _ = item.Approve(decision.ID, "")
	})
	if err != nil || waited.Status != "authorized" {
		t.Fatal(err, waited.Status)
	}
	rejected, err := item.Propose(proposal(map[string]any{
		"title": "Auth library", "kind": "change", "choice": "lucia", "paths": []string{"src/**"},
	}), revise)
	if err != nil {
		t.Fatal(err)
	}
	turned, err := item.WaitForHuman(rejected.ID, 5*time.Second, 0, func(time.Duration) {
		_, _ = item.Reject(rejected.ID, "")
	})
	if err != nil || turned.Status != "rejected" {
		t.Fatal(err, turned.Status)
	}
	stuck, err := item.Propose(proposal(map[string]any{"title": "Another stack"}), accept)
	if err != nil {
		t.Fatal(err)
	}
	_, err = item.WaitForHuman(stuck.ID, 0, 0, func(time.Duration) {})
	var gateErr *store.Error
	if !errors.As(err, &gateErr) {
		t.Fatal(err)
	}
	loaded, _ := item.Load(stuck.ID)
	if loaded.Status != "reviewed" {
		t.Fatal(loaded.Status)
	}
}

func TestIncompleteReviseIsNotStored(t *testing.T) {
	item := project(t)
	_, err := item.Propose(proposal(nil), func(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
		return model.ReviewResult{Verdict: "revise", Summary: "Something feels off.", Concerns: []string{}, Valid: true}
	})
	var gateErr *store.Error
	if !errors.As(err, &gateErr) {
		t.Fatal(err)
	}
	open, _ := item.OpenDecisions()
	if len(open) != 1 || open[0].Status != "proposed" || open[0].Jev != nil {
		t.Fatal(open)
	}
}

func TestSupersedingRemovesTheOldDecision(t *testing.T) {
	item := project(t)
	first, err := item.Propose(proposal(map[string]any{
		"title": "Application membership check", "paths": []string{"src/auth/**"},
		"implementation": []any{"auth.check:Check membership in application code"},
		"invariants":     []any{"auth.membership:A request must be authorized by current organization membership"},
	}), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.Approve(first.ID, ""); err != nil {
		t.Fatal(err)
	}
	second, err := item.Propose(proposal(map[string]any{
		"title": "Row level security", "kind": "change", "choice": "Enforce membership in row level security",
		"paths": []string{"src/auth/**"}, "supersedes": []string{first.ID},
		"implementation": []any{"auth.rls:Enforce membership with row level security"},
	}), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.Approve(second.ID, ""); err != nil {
		t.Fatal(err)
	}
	older, _ := item.Load(first.ID)
	newer, _ := item.Load(second.ID)
	if older.Status != "superseded" || older.SupersededBy != second.ID {
		t.Fatal(older.Status)
	}
	found := false
	for _, id := range newer.Supersedes {
		if id == first.ID {
			found = true
		}
	}
	if !found {
		t.Fatal(newer.Supersedes)
	}
	all, _ := item.All()
	active, _ := review.SelectActive(newer, all)
	for _, item := range active {
		if item.ID == first.ID {
			t.Fatal("superseded still active")
		}
	}
}

func TestNormalizeRequiresScope(t *testing.T) {
	_, err := store.NormalizeFields(map[string]any{
		"kind": "kickoff", "title": "A", "question": "B", "choice": "C", "paths": []string{},
	})
	var gateErr *store.Error
	if !errors.As(err, &gateErr) {
		t.Fatal(err)
	}
}
