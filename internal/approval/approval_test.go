// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
)

// fakeLister serves fixed policies and requests, so the gate's decisions can be
// exercised without an API server.
type fakeLister struct {
	policies []openchoreov1alpha1.ApprovalPolicy
	requests []openchoreov1alpha1.ApprovalRequest
}

func (f *fakeLister) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	switch l := list.(type) {
	case *openchoreov1alpha1.ApprovalPolicyList:
		l.Items = f.policies
	case *openchoreov1alpha1.ApprovalRequestList:
		l.Items = f.requests
	}
	return nil
}

func policy(name, action, env string, mods ...func(*openchoreov1alpha1.ApprovalPolicy)) openchoreov1alpha1.ApprovalPolicy {
	p := openchoreov1alpha1.ApprovalPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "acme"},
		Spec: openchoreov1alpha1.ApprovalPolicySpec{
			Action: action,
			Scope:  openchoreov1alpha1.ApprovalScope{Environment: env},
			Approvers: []openchoreov1alpha1.ApproverRef{{
				Entitlement: &openchoreov1alpha1.EntitlementClaim{Claim: "groups", Value: "release-managers"},
			}},
		},
	}
	for _, m := range mods {
		m(&p)
	}
	return p
}

func target() openchoreov1alpha1.ApprovalTarget {
	return openchoreov1alpha1.ApprovalTarget{
		Kind: "ReleaseBinding", Name: "checkout-production",
		Project: "checkout", Component: "checkout-svc", Environment: "production",
	}
}

func attempt() Attempt {
	return Attempt{
		Action:     "releasebinding:update",
		Target:     target(),
		ChangeKind: openchoreov1alpha1.ChangeKindRelease,
		Subject:    openchoreov1alpha1.ApprovalSubject{ID: "dev-1"},
	}
}

const stateV142 = `{"releaseName":"checkout-v1.4.2","replicas":3}`
const stateV143 = `{"releaseName":"checkout-v1.4.3","replicas":3}`

func TestFingerprintIsOrderIndependent(t *testing.T) {
	a, err := Fingerprint([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := Fingerprint([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != b {
		t.Fatalf("key order changed the fingerprint: %s vs %s", a, b)
	}
}

func TestFingerprintDistinguishesDifferentState(t *testing.T) {
	a, _ := Fingerprint([]byte(stateV142))
	b, _ := Fingerprint([]byte(stateV143))
	if a == b {
		t.Fatal("different releases produced the same fingerprint")
	}
}

// An environment nobody gated must behave exactly as it does today.
func TestUngatedActionPassesThrough(t *testing.T) {
	l := &fakeLister{policies: []openchoreov1alpha1.ApprovalPolicy{
		policy("staging-only", "releasebinding:update", "staging"),
	}}
	v, err := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Outcome != OutcomeNotGated || !v.Allowed() {
		t.Fatalf("expected NotGated/allowed, got %s allowed=%v", v.Outcome, v.Allowed())
	}
}

func TestGatedActionWithoutRequestIsRequired(t *testing.T) {
	l := &fakeLister{policies: []openchoreov1alpha1.ApprovalPolicy{
		policy("prod", "releasebinding:update", "production"),
	}}
	v, err := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Outcome != OutcomeRequired || v.Allowed() {
		t.Fatalf("expected ApprovalRequired and not allowed, got %s allowed=%v", v.Outcome, v.Allowed())
	}
}

func TestSuspendedPolicyDoesNotGate(t *testing.T) {
	l := &fakeLister{policies: []openchoreov1alpha1.ApprovalPolicy{
		policy("prod", "releasebinding:update", "production", func(p *openchoreov1alpha1.ApprovalPolicy) {
			p.Spec.Suspend = true
		}),
	}}
	v, _ := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
	if v.Outcome != OutcomeNotGated {
		t.Fatalf("a suspended policy still gated the action: %s", v.Outcome)
	}
}

func request(fp string, phase openchoreov1alpha1.ApprovalPhase) openchoreov1alpha1.ApprovalRequest {
	return openchoreov1alpha1.ApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "apr-1", Namespace: "acme"},
		Spec: openchoreov1alpha1.ApprovalRequestSpec{
			PolicyName:       "prod",
			Action:           "releasebinding:update",
			Target:           target(),
			StateFingerprint: fp,
			Requester:        openchoreov1alpha1.ApprovalSubject{ID: "dev-1"},
			RequestedState:   &runtime.RawExtension{Raw: []byte(stateV142)},
		},
		Status: openchoreov1alpha1.ApprovalRequestStatus{Phase: phase},
	}
}

func TestApprovedRequestAllowsExactlyThatChange(t *testing.T) {
	fp, _ := Fingerprint([]byte(stateV142))
	l := &fakeLister{
		policies: []openchoreov1alpha1.ApprovalPolicy{policy("prod", "releasebinding:update", "production")},
		requests: []openchoreov1alpha1.ApprovalRequest{request(fp, openchoreov1alpha1.ApprovalPhaseApproved)},
	}
	v, _ := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
	if v.Outcome != OutcomeApproved || !v.Allowed() {
		t.Fatalf("approved change was not allowed: %s", v.Outcome)
	}
}

// The property the whole design rests on: an approval must not be reusable for
// a change nobody reviewed.
func TestApprovalCannotBeReplayedForDifferentState(t *testing.T) {
	fp, _ := Fingerprint([]byte(stateV142))
	l := &fakeLister{
		policies: []openchoreov1alpha1.ApprovalPolicy{policy("prod", "releasebinding:update", "production")},
		requests: []openchoreov1alpha1.ApprovalRequest{request(fp, openchoreov1alpha1.ApprovalPhaseApproved)},
	}
	// Approved for v1.4.2; now try to ship v1.4.3 under the same approval.
	v, _ := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV143))
	if v.Allowed() {
		t.Fatal("an approval for one release permitted a different one")
	}
	if v.Outcome != OutcomeRequired {
		t.Fatalf("expected a fresh approval to be required, got %s", v.Outcome)
	}
}

func TestTerminalRequestDoesNotAuthoriseAgain(t *testing.T) {
	fp, _ := Fingerprint([]byte(stateV142))
	for _, phase := range []openchoreov1alpha1.ApprovalPhase{
		openchoreov1alpha1.ApprovalPhaseExecuted,
		openchoreov1alpha1.ApprovalPhaseCancelled,
		openchoreov1alpha1.ApprovalPhaseFailed,
	} {
		l := &fakeLister{
			policies: []openchoreov1alpha1.ApprovalPolicy{policy("prod", "releasebinding:update", "production")},
			requests: []openchoreov1alpha1.ApprovalRequest{request(fp, phase)},
		}
		v, _ := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
		if v.Allowed() {
			t.Fatalf("a %s request still authorised the action", phase)
		}
	}
}

func TestRejectedRequestIsReported(t *testing.T) {
	fp, _ := Fingerprint([]byte(stateV142))
	l := &fakeLister{
		policies: []openchoreov1alpha1.ApprovalPolicy{policy("prod", "releasebinding:update", "production")},
		requests: []openchoreov1alpha1.ApprovalRequest{request(fp, openchoreov1alpha1.ApprovalPhaseRejected)},
	}
	v, _ := Evaluate(context.Background(), l, "acme", attempt(), []byte(stateV142))
	if v.Outcome != OutcomeRejected || v.Allowed() {
		t.Fatalf("expected Rejected and not allowed, got %s", v.Outcome)
	}
}

// A policy that names no change kinds must cover undeploy, not just promotion.
func TestUnnarrowedPolicyGatesUndeploy(t *testing.T) {
	l := &fakeLister{policies: []openchoreov1alpha1.ApprovalPolicy{
		policy("prod", "releasebinding:update", "production"),
	}}
	a := attempt()
	a.ChangeKind = openchoreov1alpha1.ChangeKindUndeploy
	v, _ := Evaluate(context.Background(), l, "acme", a, []byte(stateV142))
	if v.Outcome != OutcomeRequired {
		t.Fatalf("undeploy slipped past an unnarrowed policy: %s", v.Outcome)
	}
}

func TestNarrowedPolicyIgnoresOtherChangeKinds(t *testing.T) {
	l := &fakeLister{policies: []openchoreov1alpha1.ApprovalPolicy{
		policy("prod", "releasebinding:update", "production", func(p *openchoreov1alpha1.ApprovalPolicy) {
			p.Spec.Changes = []openchoreov1alpha1.ChangeKind{openchoreov1alpha1.ChangeKindRelease}
		}),
	}}
	a := attempt()
	a.ChangeKind = openchoreov1alpha1.ChangeKindConfig
	v, _ := Evaluate(context.Background(), l, "acme", a, []byte(stateV142))
	if v.Outcome != OutcomeNotGated {
		t.Fatalf("a release-only policy gated a config change: %s", v.Outcome)
	}
}

func TestMostSpecificPolicyWins(t *testing.T) {
	broad := policy("broad", "releasebinding:update", "")
	narrow := policy("narrow", "releasebinding:update", "production", func(p *openchoreov1alpha1.ApprovalPolicy) {
		p.Spec.Scope.Project = "checkout"
	})
	got := MatchPolicy([]openchoreov1alpha1.ApprovalPolicy{broad, narrow}, attempt())
	if got == nil || got.Name != "narrow" {
		t.Fatalf("expected the narrower policy to win, got %v", got)
	}
}

func TestFromEnvironmentNarrowsPromotionPath(t *testing.T) {
	p := policy("dev-to-prod", "releasebinding:update", "production", func(p *openchoreov1alpha1.ApprovalPolicy) {
		p.Spec.Scope.FromEnvironment = "dev"
	})
	fromStaging := attempt()
	fromStaging.FromEnvironment = "staging"
	if MatchPolicy([]openchoreov1alpha1.ApprovalPolicy{p}, fromStaging) != nil {
		t.Fatal("a dev-scoped policy gated a promotion from staging")
	}
	fromDev := attempt()
	fromDev.FromEnvironment = "dev"
	if MatchPolicy([]openchoreov1alpha1.ApprovalPolicy{p}, fromDev) == nil {
		t.Fatal("a dev-scoped policy failed to gate a promotion from dev")
	}
}

func TestClassifyReleaseBindingChange(t *testing.T) {
	active := func(release string) *openchoreov1alpha1.ReleaseBindingSpec {
		return &openchoreov1alpha1.ReleaseBindingSpec{
			ReleaseName: release, State: openchoreov1alpha1.ReleaseStateActive,
		}
	}
	cases := []struct {
		name     string
		old, new *openchoreov1alpha1.ReleaseBindingSpec
		want     openchoreov1alpha1.ChangeKind
	}{
		{"promotion", active("v1"), active("v2"), openchoreov1alpha1.ChangeKindRelease},
		{"rollback is a release change", active("v2"), active("v1"), openchoreov1alpha1.ChangeKindRelease},
		{"first bind", nil, active("v1"), openchoreov1alpha1.ChangeKindRelease},
		{"config only", active("v1"), active("v1"), openchoreov1alpha1.ChangeKindConfig},
		{"undeploy", active("v1"), &openchoreov1alpha1.ReleaseBindingSpec{
			ReleaseName: "v1", State: openchoreov1alpha1.ReleaseStateUndeploy,
		}, openchoreov1alpha1.ChangeKindUndeploy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyReleaseBindingChange(c.old, c.new); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestSelfApprovalBlockedByDefault(t *testing.T) {
	p := policy("prod", "releasebinding:update", "production")
	fp, _ := Fingerprint([]byte(stateV142))
	req := request(fp, openchoreov1alpha1.ApprovalPhasePending)
	self := openchoreov1alpha1.ApprovalSubject{ID: "dev-1"} // same as requester

	ok, reason := CanDecide(&p, &req, self, map[string][]string{"groups": {"release-managers"}}, false)
	if ok {
		t.Fatal("requester approved their own request under the default policy")
	}
	if reason == "" {
		t.Fatal("refusal gave no reason to show the user")
	}

	p.Spec.AllowSelfApproval = true
	if ok, _ := CanDecide(&p, &req, self, map[string][]string{"groups": {"release-managers"}}, false); !ok {
		t.Fatal("self-approval refused even though the policy permits it")
	}
}

func TestCanDecideMatchesEntitlementAndRole(t *testing.T) {
	p := policy("prod", "releasebinding:update", "production")
	fp, _ := Fingerprint([]byte(stateV142))
	req := request(fp, openchoreov1alpha1.ApprovalPhasePending)
	approver := openchoreov1alpha1.ApprovalSubject{ID: "lead-9"}

	if ok, _ := CanDecide(&p, &req, approver, map[string][]string{"groups": {"release-managers"}}, false); !ok {
		t.Fatal("a matching group claim was not accepted")
	}
	if ok, _ := CanDecide(&p, &req, approver, map[string][]string{"groups": {"interns"}}, false); ok {
		t.Fatal("a non-matching claim was accepted")
	}

	rolePolicy := p
	rolePolicy.Spec.Approvers = []openchoreov1alpha1.ApproverRef{{
		RoleRef: &openchoreov1alpha1.RoleRef{
			Kind: openchoreov1alpha1.RoleRefKindClusterAuthzRole, Name: "release-manager",
		},
	}}
	if ok, _ := CanDecide(&rolePolicy, &req, approver, nil, true); !ok {
		t.Fatal("a role-holder was refused")
	}
	if ok, _ := CanDecide(&rolePolicy, &req, approver, nil, false); ok {
		t.Fatal("a non-role-holder was accepted")
	}
}

func TestCannotDecideANonPendingRequest(t *testing.T) {
	p := policy("prod", "releasebinding:update", "production")
	fp, _ := Fingerprint([]byte(stateV142))
	req := request(fp, openchoreov1alpha1.ApprovalPhaseApproved)
	ok, _ := CanDecide(&p, &req, openchoreov1alpha1.ApprovalSubject{ID: "lead-9"},
		map[string][]string{"groups": {"release-managers"}}, false)
	if ok {
		t.Fatal("an already-decided request accepted a second decision")
	}
}
