// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ApprovalPhase is the lifecycle position of an ApprovalRequest.
//
// Pending -> Approved -> Executed, or Pending -> Rejected | Cancelled.
//
// Approved and Executed are distinct because approving and performing are
// distinct events. This slice unlocks the action on approval and lets the
// requester retry, so Executed is reached when that retry succeeds; the phase
// set does not change if execution later becomes automatic.
// +kubebuilder:validation:Enum=Pending;Approved;Rejected;Cancelled;Executed;Failed
type ApprovalPhase string

const (
	ApprovalPhasePending   ApprovalPhase = "Pending"
	ApprovalPhaseApproved  ApprovalPhase = "Approved"
	ApprovalPhaseRejected  ApprovalPhase = "Rejected"
	ApprovalPhaseCancelled ApprovalPhase = "Cancelled"
	ApprovalPhaseExecuted  ApprovalPhase = "Executed"
	ApprovalPhaseFailed    ApprovalPhase = "Failed"
)

// IsTerminal reports whether the phase admits no further transition.
func (p ApprovalPhase) IsTerminal() bool {
	switch p {
	case ApprovalPhaseRejected, ApprovalPhaseCancelled, ApprovalPhaseExecuted, ApprovalPhaseFailed:
		return true
	default:
		return false
	}
}

// ApprovalResult is an approver's answer.
// +kubebuilder:validation:Enum=Approved;Rejected
type ApprovalResult string

const (
	ApprovalResultApproved ApprovalResult = "Approved"
	ApprovalResultRejected ApprovalResult = "Rejected"
)

// ApprovalSubject identifies a person as resolved from their token. It is not a
// reference to a stored user record: OpenChoreo has no user directory, so the
// display name is captured at the time for later history.
type ApprovalSubject struct {
	// ID is the stable subject identifier, from the token's `sub` claim.
	// +required
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`

	// DisplayName is a human-readable name captured when the subject acted.
	// +optional
	DisplayName string `json:"displayName,omitempty"`
}

// ApprovalTarget identifies the object an action would act upon.
type ApprovalTarget struct {
	// Kind of the target object, e.g. "ReleaseBinding".
	// +required
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Name of the target object.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Project owning the target.
	// +optional
	Project string `json:"project,omitempty"`

	// Component owning the target.
	// +optional
	Component string `json:"component,omitempty"`

	// Environment the action targets. Drives approver matching, and is the field
	// operators filter on.
	// +optional
	Environment string `json:"environment,omitempty"`
}

// ApprovalSummary is the evidence shown to an approver. It is held on the
// request rather than resolved live so that an approver can decide without read
// access to the requester's project, and so the record preserves what they were
// actually shown at the moment they decided.
type ApprovalSummary struct {
	// ChangeKind classifies the change.
	// +optional
	ChangeKind ChangeKind `json:"changeKind,omitempty"`

	// Current describes what is running now.
	// +optional
	Current string `json:"current,omitempty"`

	// Requested describes what would run.
	// +optional
	Requested string `json:"requested,omitempty"`

	// Details holds rendered lines describing the difference. Without these an
	// approver is deciding on a version string alone.
	// +optional
	Details []string `json:"details,omitempty"`
}

// ApprovalDecision records an approver's answer.
// +kubebuilder:validation:XValidation:rule="self.result != 'Rejected' || (has(self.comment) && size(self.comment) > 0)",message="a rejection requires a comment"
type ApprovalDecision struct {
	// Result is the answer.
	// +required
	Result ApprovalResult `json:"result"`

	// DecidedBy is the approving subject.
	// +required
	DecidedBy ApprovalSubject `json:"decidedBy"`

	// DecidedAt is when the decision was made.
	// +required
	DecidedAt metav1.Time `json:"decidedAt"`

	// Comment explains the decision. Required on rejection: a rejection with no
	// reason is the worst outcome for the requester.
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	Comment string `json:"comment,omitempty"`
}

// ApprovalRequestSpec defines the desired state of ApprovalRequest.
//
// Every field except Decision is immutable: an approver must not be able to
// review one change while a different one executes.
type ApprovalRequestSpec struct {
	// PolicyName is the ApprovalPolicy that gated the action, in this namespace.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.policyName is immutable"
	PolicyName string `json:"policyName"`

	// Action is the gated action, copied from the policy at creation so the
	// request stays meaningful if the policy later changes.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.action is immutable"
	Action string `json:"action"`

	// Target identifies the object the action would act upon.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.target is immutable"
	Target ApprovalTarget `json:"target"`

	// RequestedState is the complete intent being approved. For a promotion this
	// is the whole desired binding spec, not just the release name: binding an
	// approval to the release name alone would let environment configs or
	// workload overrides change between review and execution.
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	RequestedState *runtime.RawExtension `json:"requestedState"`

	// StateFingerprint is a digest of RequestedState, computed at creation. The
	// action is only permitted to proceed when the state being applied hashes to
	// this value, which is what stops an approval being reused for a different
	// change.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.stateFingerprint is immutable"
	StateFingerprint string `json:"stateFingerprint"`

	// Summary is the rendered evidence shown to an approver.
	// +optional
	Summary *ApprovalSummary `json:"summary,omitempty"`

	// Requester is the subject that opened the request.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.requester is immutable"
	Requester ApprovalSubject `json:"requester"`

	// Message is the requester's justification, and the single largest lever on
	// how quickly a request is decided.
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	Message string `json:"message,omitempty"`

	// Decision records the approver's answer. Absent while pending, set once by
	// the API on behalf of an authenticated approver, and immutable thereafter.
	//
	// It is intent rather than observation, so it lives in spec. The consequence
	// is that a subject with direct write access to the CR could forge one, which
	// under an API-layer gate is the same population that can bypass the gate
	// outright.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(oldSelf) || self == oldSelf",message="spec.decision is immutable once set"
	Decision *ApprovalDecision `json:"decision,omitempty"`

	// TTLAfterCompletion is how long this request is retained after reaching a
	// terminal phase. Copied from the policy at creation.
	// +optional
	// +kubebuilder:validation:Pattern=`^(\d+d)?(\d+h)?(\d+m)?(\d+s)?$`
	TTLAfterCompletion string `json:"ttlAfterCompletion,omitempty"`
}

// ApprovalRequestStatus defines the observed state of ApprovalRequest.
type ApprovalRequestStatus struct {
	// Phase is the current lifecycle position.
	// +optional
	Phase ApprovalPhase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// CompletedAt is when the request reached a terminal phase, used with
	// TTLAfterCompletion to decide when to delete it.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// StaleReason is set when a pending request can no longer be fulfilled as
	// written, such as the requested release having been deleted or superseded.
	// Surfaced rather than auto-resolved: the approver reviewed a specific
	// artifact, so the request is never silently retargeted.
	// +optional
	StaleReason string `json:"staleReason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=apreq;apreqs
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.name`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.target.environment`
// +kubebuilder:printcolumn:name="Requester",type=string,JSONPath=`.spec.requester.displayName`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ApprovalRequest is the Schema for the approvalrequests API.
// It represents one pending human decision about one specific change.
type ApprovalRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApprovalRequestSpec   `json:"spec,omitempty"`
	Status ApprovalRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApprovalRequestList contains a list of ApprovalRequest.
type ApprovalRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApprovalRequest{}, &ApprovalRequestList{})
}
