// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChangeKind classifies a change to a gated target. A policy that lists no
// change kinds gates every kind, which is the safe default: a policy written
// only against ReleaseChange would leave undeploy and config-only edits
// ungated, and either can take an environment down as surely as a bad release.
// +kubebuilder:validation:Enum=ReleaseChange;ConfigChange;Undeploy
type ChangeKind string

const (
	// ChangeKindRelease is a change to the release pinned by the binding.
	// Rollback is a ReleaseChange: it is the same field moving backwards.
	ChangeKindRelease ChangeKind = "ReleaseChange"

	// ChangeKindConfig is a change to environment configs, trait configs or
	// workload overrides that leaves the pinned release untouched.
	ChangeKindConfig ChangeKind = "ConfigChange"

	// ChangeKindUndeploy is removing the workload from the environment.
	ChangeKindUndeploy ChangeKind = "Undeploy"
)

// AllChangeKinds is the set applied when a policy does not narrow itself.
func AllChangeKinds() []ChangeKind {
	return []ChangeKind{ChangeKindRelease, ChangeKindConfig, ChangeKindUndeploy}
}

// ApprovalScope narrows an ApprovalPolicy to part of the ownership hierarchy.
// It is deliberately not TargetScope: approvals are almost always scoped by
// environment, which TargetScope has no dimension for.
type ApprovalScope struct {
	// Project scopes the policy to a single project.
	// +optional
	Project string `json:"project,omitempty"`

	// Component scopes the policy to a single component. Requires Project.
	// +optional
	Component string `json:"component,omitempty"`

	// Environment scopes the policy to the environment being acted upon.
	// +optional
	Environment string `json:"environment,omitempty"`

	// FromEnvironment further narrows to promotions originating in a specific
	// environment, so "staging to production" can be permitted while
	// "dev to production" is gated.
	// +optional
	FromEnvironment string `json:"fromEnvironment,omitempty"`
}

// ApproverRef identifies who may decide. Exactly one of RoleRef or Entitlement
// must be set: a role grants approval authority to whoever holds it, while an
// entitlement names a subject directly.
//
// A claim-matched subject cannot be enumerated. The platform can test whether a
// caller matches, but cannot list who matches, so a named individual is the only
// form that lets a requester see who to ask.
// +kubebuilder:validation:XValidation:rule="has(self.roleRef) != has(self.entitlement)",message="exactly one of roleRef or entitlement must be set"
type ApproverRef struct {
	// RoleRef grants approval authority to holders of an AuthzRole or ClusterAuthzRole.
	// +optional
	RoleRef *RoleRef `json:"roleRef,omitempty"`

	// Scope bounds where a role-based approver's authority applies. Ignored when
	// Entitlement is set. Omitted means the policy's own scope.
	// +optional
	Scope *TargetScope `json:"scope,omitempty"`

	// Entitlement names a subject by JWT claim, e.g.
	// {claim: email, value: someone@example.com} for a person, or
	// {claim: groups, value: sre-leads} for a group.
	// +optional
	Entitlement *EntitlementClaim `json:"entitlement,omitempty"`
}

// ApprovalPolicySpec defines the desired state of ApprovalPolicy.
type ApprovalPolicySpec struct {
	// Action is the action this policy gates, e.g. "releasebinding:update".
	// Validated against the registry of approval-capable actions: gating an action
	// with no enforcement point would produce a policy that silently never fires.
	// +required
	// +kubebuilder:validation:MinLength=1
	Action string `json:"action"`

	// Scope narrows the policy to part of the hierarchy. An empty scope gates the
	// action everywhere in the namespace.
	// +optional
	Scope ApprovalScope `json:"scope,omitempty"`

	// Changes selects which kinds of change are gated. Omitted gates every kind.
	// +optional
	// +kubebuilder:validation:MinItems=1
	Changes []ChangeKind `json:"changes,omitempty"`

	// Approvers lists who may decide. Any one of them is sufficient.
	// +required
	// +kubebuilder:validation:MinItems=1
	Approvers []ApproverRef `json:"approvers"`

	// AllowSelfApproval permits the requester to approve their own request.
	// +optional
	// +kubebuilder:default=false
	AllowSelfApproval bool `json:"allowSelfApproval,omitempty"`

	// Suspend stops the policy gating anything without deleting it, so a policy's
	// history survives an incident. Pending requests are unaffected.
	// +optional
	// +kubebuilder:default=false
	Suspend bool `json:"suspend,omitempty"`

	// RequestTTLAfterCompletion is how long decided requests from this policy are
	// retained before deletion. Pending requests are never auto-deleted.
	// Format matches WorkflowRun: "90d", "1h30m", "30m".
	// +optional
	// +kubebuilder:default="30d"
	// +kubebuilder:validation:Pattern=`^(\d+d)?(\d+h)?(\d+m)?(\d+s)?$`
	RequestTTLAfterCompletion string `json:"requestTTLAfterCompletion,omitempty"`
}

// ApprovalPolicyStatus defines the observed state of ApprovalPolicy.
type ApprovalPolicyStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the policy.
	// Surfaced here rather than left for a developer to discover mid-incident:
	// ActionNotGatable, RoleNotFound, ApproversUnresolvable.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=appol;appols
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.scope.environment`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ApprovalPolicy is the Schema for the approvalpolicies API.
// It declares that an action, within a scope, requires a human decision.
type ApprovalPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApprovalPolicySpec   `json:"spec,omitempty"`
	Status ApprovalPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApprovalPolicyList contains a list of ApprovalPolicy.
type ApprovalPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApprovalPolicy{}, &ApprovalPolicyList{})
}
