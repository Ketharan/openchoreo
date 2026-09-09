// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
)

// MessageAnnotation carries the requester's justification into the request.
//
// It is an annotation rather than an API field so that the same justification
// arrives whichever way the change was made — occ, the portal, a direct API
// call, or kubectl — without every gated resource's API growing a field that
// only matters while a gate is in play.
const MessageAnnotation = "openchoreo.dev/approval-message"

// Creator is the write half of the client the opener needs. Narrower than
// client.Client so tests can substitute a fake.
type Creator interface {
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Status() client.SubResourceWriter
}

// OpenParams describes the request to raise for a blocked action.
type OpenParams struct {
	Namespace string

	// Policy is the policy that gated the action.
	Policy *openchoreov1alpha1.ApprovalPolicy

	// Attempt is the action that was blocked.
	Attempt Attempt

	// RequestedState is the complete intent being asked for.
	RequestedState []byte

	// Fingerprint is the digest of RequestedState, from the gate's verdict.
	Fingerprint string

	// Summary is the rendered evidence for the approver. Optional, but a request
	// without one asks somebody to approve a name they cannot evaluate.
	Summary *openchoreov1alpha1.ApprovalSummary

	// Message is the requester's justification.
	Message string
}

// Open raises an ApprovalRequest for a blocked action.
//
// The name is generated rather than derived from the change, so that a request
// can be raised again after an earlier one for the same change was rejected or
// cancelled. A deterministic name would collide with that spent request and
// leave the requester unable to ask a second time.
func Open(ctx context.Context, c Creator, p OpenParams) (*openchoreov1alpha1.ApprovalRequest, error) {
	if p.Policy == nil {
		return nil, fmt.Errorf("cannot open an approval request without a policy")
	}
	if p.Fingerprint == "" {
		return nil, fmt.Errorf("cannot open an approval request without a state fingerprint")
	}

	req := &openchoreov1alpha1.ApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "apr-",
			Namespace:    p.Namespace,
		},
		Spec: openchoreov1alpha1.ApprovalRequestSpec{
			PolicyName:       p.Policy.Name,
			Action:           p.Attempt.Action,
			Target:           p.Attempt.Target,
			RequestedState:   &runtime.RawExtension{Raw: p.RequestedState},
			StateFingerprint: p.Fingerprint,
			Summary:          p.Summary,
			Requester:        p.Attempt.Subject,
			Message:          p.Message,
			// Copied from the policy so the request keeps its retention even if the
			// policy is later edited or deleted.
			TTLAfterCompletion: p.Policy.Spec.RequestTTLAfterCompletion,
		},
	}

	if err := c.Create(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to open approval request: %w", err)
	}

	// Persist the phase immediately rather than waiting for a controller. Without
	// this the stored request has no status, and a listing shows a blank phase to
	// the very approver being asked to act on it.
	//
	// A failure here is not fatal: the request exists, and the gate reads an unset
	// phase as Pending precisely so a missed status write cannot cause a second
	// request to be opened for the same change.
	req.Status.Phase = openchoreov1alpha1.ApprovalPhasePending
	if err := c.Status().Update(ctx, req); err != nil {
		return req, fmt.Errorf("approval request %s opened, but its status could not be set: %w", req.Name, err)
	}
	return req, nil
}
