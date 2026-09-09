// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"sort"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
)

// Attempt describes an action a subject is trying to take, in the terms the
// gate needs to evaluate it.
type Attempt struct {
	// Action is the authz action being attempted, e.g. "releasebinding:update".
	Action string

	// Target identifies the object being acted upon.
	Target openchoreov1alpha1.ApprovalTarget

	// FromEnvironment is the environment a promotion originates in, where that
	// is known. Empty when the action has no source environment.
	FromEnvironment string

	// ChangeKind classifies what sort of change this is.
	ChangeKind openchoreov1alpha1.ChangeKind

	// Subject is who is attempting the action.
	Subject openchoreov1alpha1.ApprovalSubject
}

// gatesChangeKind reports whether the policy covers this kind of change. A
// policy that lists no change kinds covers all of them: narrowing has to be
// deliberate, because a policy written only against release changes would leave
// undeploy and config-only edits ungated.
func gatesChangeKind(p *openchoreov1alpha1.ApprovalPolicy, kind openchoreov1alpha1.ChangeKind) bool {
	if len(p.Spec.Changes) == 0 {
		return true
	}
	for _, c := range p.Spec.Changes {
		if c == kind {
			return true
		}
	}
	return false
}

// scopeSpecificity scores how narrowly a policy's scope is drawn, so that the
// most specific matching policy wins rather than an arbitrary one.
func scopeSpecificity(s openchoreov1alpha1.ApprovalScope) int {
	n := 0
	for _, f := range []string{s.Project, s.Component, s.Environment, s.FromEnvironment} {
		if f != "" {
			n++
		}
	}
	return n
}

// scopeMatches reports whether a policy's scope covers the attempt. An empty
// field matches anything, so an empty scope gates the action namespace-wide.
func scopeMatches(s openchoreov1alpha1.ApprovalScope, a Attempt) bool {
	if s.Project != "" && s.Project != a.Target.Project {
		return false
	}
	if s.Component != "" && s.Component != a.Target.Component {
		return false
	}
	if s.Environment != "" && s.Environment != a.Target.Environment {
		return false
	}
	if s.FromEnvironment != "" && s.FromEnvironment != a.FromEnvironment {
		return false
	}
	return true
}

// MatchPolicy returns the policy that gates this attempt, or nil if none does.
//
// Suspended policies never match, which is how a platform engineer disables a
// gate during an incident without deleting it and losing its history.
//
// Where several policies match, the most specifically scoped one wins; ties are
// broken by name so the choice is deterministic rather than dependent on list
// ordering. Overlapping policies combining as AND, so that every matching policy
// must approve, is a plausible alternative that is deliberately not implemented
// here: it needs a decision about how a request records multiple policies.
func MatchPolicy(policies []openchoreov1alpha1.ApprovalPolicy, a Attempt) *openchoreov1alpha1.ApprovalPolicy {
	var matched []*openchoreov1alpha1.ApprovalPolicy

	for i := range policies {
		p := &policies[i]
		if p.Spec.Suspend {
			continue
		}
		if p.Spec.Action != a.Action {
			continue
		}
		if !gatesChangeKind(p, a.ChangeKind) {
			continue
		}
		if !scopeMatches(p.Spec.Scope, a) {
			continue
		}
		matched = append(matched, p)
	}

	if len(matched) == 0 {
		return nil
	}

	sort.SliceStable(matched, func(i, j int) bool {
		si, sj := scopeSpecificity(matched[i].Spec.Scope), scopeSpecificity(matched[j].Spec.Scope)
		if si != sj {
			return si > sj
		}
		return matched[i].Name < matched[j].Name
	})

	return matched[0]
}

// ClassifyReleaseBindingChange determines what kind of change is being made to
// a ReleaseBinding, so the gate can tell a promotion from a config edit or an
// undeploy. A nil old spec is a first bind, treated as a release change.
//
// Rollback is deliberately not a distinct kind: it is the release pin moving
// backwards, and a policy that gates release changes should catch it.
func ClassifyReleaseBindingChange(oldSpec, newSpec *openchoreov1alpha1.ReleaseBindingSpec) openchoreov1alpha1.ChangeKind {
	if newSpec == nil {
		return openchoreov1alpha1.ChangeKindRelease
	}
	if newSpec.State == openchoreov1alpha1.ReleaseStateUndeploy &&
		(oldSpec == nil || oldSpec.State != openchoreov1alpha1.ReleaseStateUndeploy) {
		return openchoreov1alpha1.ChangeKindUndeploy
	}
	if oldSpec == nil || oldSpec.ReleaseName != newSpec.ReleaseName {
		return openchoreov1alpha1.ChangeKindRelease
	}
	return openchoreov1alpha1.ChangeKindConfig
}
