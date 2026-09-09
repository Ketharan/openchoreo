// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"context"
	"errors"
	"fmt"
)

// ErrApprovalRequired is returned when a gated action has no approval yet. It is
// deliberately distinct from a permission error: the subject may well be allowed
// to do this, they simply have not been told yes yet, and a caller that renders
// it as "forbidden" tells the user something untrue.
var ErrApprovalRequired = errors.New("this action requires approval")

// ErrApprovalPending is returned when a request exists and is awaiting a decision.
var ErrApprovalPending = errors.New("this action is awaiting approval")

// ErrApprovalRejected is returned when an approver refused this change.
var ErrApprovalRejected = errors.New("this action was rejected by an approver")

// GateError carries the verdict alongside the sentinel error, so a handler can
// render the request name, the approvers and the reason rather than a bare
// refusal. Callers match the sentinel with errors.Is and reach the detail with
// errors.As.
type GateError struct {
	Err     error
	Verdict Verdict
}

func (e *GateError) Error() string {
	if e.Verdict.Request != nil {
		return fmt.Sprintf("%v (request %s)", e.Err, e.Verdict.Request.Name)
	}
	if e.Verdict.Policy != nil {
		return fmt.Sprintf("%v (policy %s)", e.Err, e.Verdict.Policy.Name)
	}
	return e.Err.Error()
}

func (e *GateError) Unwrap() error { return e.Err }

// Enforce evaluates the gate and returns nil when the action may proceed.
//
// It never opens a request. Creating one is a separate, explicit step, because
// a caller doing a pre-flight check ("would this need approval?") must not
// leave a pending request behind as a side effect.
func Enforce(ctx context.Context, r Lister, namespace string, a Attempt, requestedState []byte) error {
	v, err := Evaluate(ctx, r, namespace, a, requestedState)
	if err != nil {
		// Fail closed. If the gate cannot be evaluated we do not know whether this
		// action needed approval, and guessing "no" silently disables the control.
		return fmt.Errorf("evaluating approval gate: %w", err)
	}

	return AsError(v)
}

// AsError turns a blocking verdict into the error a caller should return. A
// verdict that allows the action yields nil, so callers can hand any verdict
// here without first re-testing it.
func AsError(v Verdict) error {
	switch v.Outcome {
	case OutcomeNotGated, OutcomeApproved:
		return nil
	case OutcomePending:
		return &GateError{Err: ErrApprovalPending, Verdict: v}
	case OutcomeRejected:
		return &GateError{Err: ErrApprovalRejected, Verdict: v}
	default:
		return &GateError{Err: ErrApprovalRequired, Verdict: v}
	}
}
