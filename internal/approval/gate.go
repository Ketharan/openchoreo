// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
)

// Outcome is what the gate decided about an attempted action.
type Outcome string

const (
	// OutcomeNotGated means no policy covers this action: it proceeds untouched.
	// Actions with no policy must be indistinguishable from today.
	OutcomeNotGated Outcome = "NotGated"

	// OutcomeApproved means an approval exists for exactly this change and the
	// action may proceed.
	OutcomeApproved Outcome = "Approved"

	// OutcomePending means a request exists and is awaiting a decision.
	OutcomePending Outcome = "Pending"

	// OutcomeRequired means the action is gated and no request exists yet, so one
	// should be opened.
	OutcomeRequired Outcome = "ApprovalRequired"

	// OutcomeRejected means an approver refused this change.
	OutcomeRejected Outcome = "Rejected"
)

// Verdict is the gate's answer, carrying enough context for a caller to render
// a useful message rather than a bare denial.
type Verdict struct {
	Outcome Outcome

	// Policy is the policy that gated the attempt, nil when not gated.
	Policy *openchoreov1alpha1.ApprovalPolicy

	// Request is the request covering this change, when one exists.
	Request *openchoreov1alpha1.ApprovalRequest

	// Fingerprint of the state the attempt would apply.
	Fingerprint string
}

// Allowed reports whether the action may proceed.
func (v Verdict) Allowed() bool {
	return v.Outcome == OutcomeNotGated || v.Outcome == OutcomeApproved
}

// Lister reads the policies and requests the gate evaluates. Narrower than
// client.Client so callers can substitute a fake in tests without a live API.
type Lister interface {
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// Evaluate decides whether an attempted action may proceed.
//
// It is deliberately read-only: it never opens a request, so it is safe to call
// from a pre-flight check ("would this need approval?") as well as from the
// enforcement path. Opening a request is the caller's decision, because only the
// caller knows whether the subject actually intends to go ahead.
func Evaluate(ctx context.Context, r Lister, namespace string, a Attempt, requestedState []byte) (Verdict, error) {
	fp, err := Fingerprint(requestedState)
	if err != nil {
		return Verdict{}, fmt.Errorf("fingerprinting requested state: %w", err)
	}

	var policies openchoreov1alpha1.ApprovalPolicyList
	if err := r.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		return Verdict{}, fmt.Errorf("listing approval policies: %w", err)
	}

	policy := MatchPolicy(policies.Items, a)
	if policy == nil {
		return Verdict{Outcome: OutcomeNotGated, Fingerprint: fp}, nil
	}

	var requests openchoreov1alpha1.ApprovalRequestList
	if err := r.List(ctx, &requests, client.InNamespace(namespace)); err != nil {
		return Verdict{}, fmt.Errorf("listing approval requests: %w", err)
	}

	// A request only speaks for the exact change it was raised against, so the
	// fingerprint has to match as well as the target. This is what stops an
	// approval for one release being reused to ship another.
	match := findRequest(requests.Items, a.Target, fp)
	if match == nil {
		return Verdict{Outcome: OutcomeRequired, Policy: policy, Fingerprint: fp}, nil
	}

	v := Verdict{Policy: policy, Request: match, Fingerprint: fp}
	switch match.Status.Phase {
	case openchoreov1alpha1.ApprovalPhaseApproved:
		v.Outcome = OutcomeApproved
	case openchoreov1alpha1.ApprovalPhaseRejected:
		v.Outcome = OutcomeRejected
	// An unset phase is a request that exists but has not been reconciled or had
	// its status written yet. Reading it as Pending keeps a missed status write
	// from looking like "no request", which would open a second one on every
	// subsequent attempt.
	case openchoreov1alpha1.ApprovalPhasePending, "":
		v.Outcome = OutcomePending
	default:
		// Executed, Cancelled or Failed: the request is spent, so a fresh attempt
		// needs a fresh request rather than riding on a terminal one.
		v.Outcome = OutcomeRequired
		v.Request = nil
	}
	return v, nil
}

// requestPrecedence ranks a request's phase for selection. The same change can
// accumulate several requests over time — rejected once, asked again — so the
// one that governs the current attempt is the most authoritative live one, not
// whichever the API server happened to list first.
func requestPrecedence(phase openchoreov1alpha1.ApprovalPhase) int {
	switch phase {
	case openchoreov1alpha1.ApprovalPhaseApproved:
		return 3
	case openchoreov1alpha1.ApprovalPhasePending, "":
		return 2
	case openchoreov1alpha1.ApprovalPhaseRejected:
		return 1
	default:
		// Executed, Cancelled, Failed: spent, and only chosen if nothing else matches.
		return 0
	}
}

// findRequest returns the request that governs this change, if any.
func findRequest(
	items []openchoreov1alpha1.ApprovalRequest,
	target openchoreov1alpha1.ApprovalTarget,
	fingerprint string,
) *openchoreov1alpha1.ApprovalRequest {
	var best *openchoreov1alpha1.ApprovalRequest
	bestRank := -1

	for i := range items {
		req := &items[i]
		if req.Spec.Target.Kind != target.Kind || req.Spec.Target.Name != target.Name {
			continue
		}
		if !FingerprintMatches(req.Spec.StateFingerprint, fingerprint) {
			continue
		}
		if rank := requestPrecedence(req.Status.Phase); rank > bestRank {
			best, bestRank = req, rank
		}
	}
	return best
}

// CanDecide reports whether a subject may decide a request under a policy.
//
// Approver matching by entitlement is exact: the subject's claims are compared
// against the policy's named subjects. Role-based approvers are resolved by the
// caller through the existing authz PDP, which is why this takes the result
// rather than evaluating roles itself — the gate has no business reimplementing
// permission evaluation that Casbin already does.
func CanDecide(
	policy *openchoreov1alpha1.ApprovalPolicy,
	request *openchoreov1alpha1.ApprovalRequest,
	subject openchoreov1alpha1.ApprovalSubject,
	claims map[string][]string,
	holdsApproverRole bool,
) (bool, string) {
	if policy == nil || request == nil {
		return false, "no policy or request"
	}
	if request.Status.Phase != openchoreov1alpha1.ApprovalPhasePending {
		return false, fmt.Sprintf("request is %s, not pending", request.Status.Phase)
	}
	if !policy.Spec.AllowSelfApproval && subject.ID == request.Spec.Requester.ID {
		return false, "self-approval is not permitted by this policy"
	}

	for _, ap := range policy.Spec.Approvers {
		if ap.Entitlement != nil {
			for _, v := range claims[ap.Entitlement.Claim] {
				if v == ap.Entitlement.Value {
					return true, ""
				}
			}
			continue
		}
		if ap.RoleRef != nil && holdsApproverRole {
			return true, ""
		}
	}
	return false, "subject is not an approver for this policy"
}
