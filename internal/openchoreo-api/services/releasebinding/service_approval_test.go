// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package releasebinding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/approval"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services/releasebinding/mocks"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services/testutil"
)

// prodPolicy gates any update to a binding in the "dev" environment, which is
// where testRB lives.
func prodPolicy() *openchoreov1alpha1.ApprovalPolicy {
	return &openchoreov1alpha1.ApprovalPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "gate-dev", Namespace: "ns-1"},
		Spec: openchoreov1alpha1.ApprovalPolicySpec{
			Action: "releasebinding:update",
			Scope:  openchoreov1alpha1.ApprovalScope{Environment: "dev"},
			Approvers: []openchoreov1alpha1.ApproverRef{{
				Entitlement: &openchoreov1alpha1.EntitlementClaim{Claim: "groups", Value: "release-managers"},
			}},
		},
	}
}

func approvedRequestFor(t *testing.T, rb *openchoreov1alpha1.ReleaseBinding) *openchoreov1alpha1.ApprovalRequest {
	t.Helper()
	raw, err := json.Marshal(rb.Spec)
	require.NoError(t, err)
	fp, err := approval.Fingerprint(raw)
	require.NoError(t, err)

	return &openchoreov1alpha1.ApprovalRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "apr-1", Namespace: "ns-1"},
		Spec: openchoreov1alpha1.ApprovalRequestSpec{
			PolicyName: "gate-dev",
			Action:     "releasebinding:update",
			Target: openchoreov1alpha1.ApprovalTarget{
				Kind: "ReleaseBinding", Name: rb.Name,
				Project: "my-proj", Component: "my-comp", Environment: "dev",
			},
			StateFingerprint: fp,
			Requester:        openchoreov1alpha1.ApprovalSubject{ID: "dev-1"},
		},
		Status: openchoreov1alpha1.ApprovalRequestStatus{
			Phase: openchoreov1alpha1.ApprovalPhaseApproved,
		},
	}
}

func svcWith(t *testing.T, mockSvc *mocks.MockService, objs ...client.Object) *releaseBindingServiceWithAuthz {
	t.Helper()
	return &releaseBindingServiceWithAuthz{
		internal:  mockSvc,
		k8sClient: testutil.NewFakeClient(objs...),
		authz:     testutil.NewTestAuthzChecker(testutil.AllowPDP()),
	}
}

// A subject who passes the permission check must still be stopped by the gate.
func TestUpdateBlockedWhenApprovalRequired(t *testing.T) {
	rb := testRB()
	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(rb, nil)
	// No expectation is set for UpdateReleaseBinding: mockery fails the test if the
	// gated update reaches the underlying service, which is exactly the property
	// under test.

	svc := svcWith(t, mockSvc, prodPolicy())

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)

	var gateErr *approval.GateError
	require.ErrorAs(t, err, &gateErr)
	require.NotNil(t, gateErr.Verdict.Policy)
	require.Equal(t, "gate-dev", gateErr.Verdict.Policy.Name)
}

// An environment nobody gated must behave exactly as it did before.
func TestUpdateProceedsWhenNoPolicyMatches(t *testing.T) {
	rb := testRB()
	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(rb, nil)
	mockSvc.On("UpdateReleaseBinding", mock.Anything, "ns-1", rb).Return(rb, nil)

	other := prodPolicy()
	other.Spec.Scope.Environment = "production" // does not cover this binding

	svc := svcWith(t, mockSvc, other)

	result, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.NoError(t, err)
	require.Equal(t, rb, result)
}

func TestUpdateProceedsWithMatchingApproval(t *testing.T) {
	rb := testRB()
	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(rb, nil)
	mockSvc.On("UpdateReleaseBinding", mock.Anything, "ns-1", rb).Return(rb, nil)

	svc := svcWith(t, mockSvc, prodPolicy(), approvedRequestFor(t, rb))

	result, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.NoError(t, err)
	require.Equal(t, rb, result)
}

// The replay property, exercised through the real service rather than the gate
// alone: an approval granted for one state must not let a different one through.
func TestUpdateBlockedWhenApprovalCoversDifferentState(t *testing.T) {
	approved := testRB()
	approved.Spec.ReleaseName = "checkout-v1.4.2"

	incoming := testRB()
	incoming.Spec.ReleaseName = "checkout-v1.4.3" // not what was approved

	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(approved, nil)

	svc := svcWith(t, mockSvc, prodPolicy(), approvedRequestFor(t, approved))

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", incoming)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)
}

// A suspended policy stops gating, so an incident can be worked without
// deleting the policy and losing its history.
func TestUpdateProceedsWhenPolicySuspended(t *testing.T) {
	rb := testRB()
	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(rb, nil)
	mockSvc.On("UpdateReleaseBinding", mock.Anything, "ns-1", rb).Return(rb, nil)

	suspended := prodPolicy()
	suspended.Spec.Suspend = true

	svc := svcWith(t, mockSvc, suspended)

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.NoError(t, err)
}

// The point of opening a request automatically: a developer who is blocked ends
// up with something an approver can act on, rather than an error and no route
// forward.
func TestBlockedUpdateOpensAnApprovalRequest(t *testing.T) {
	rb := testRB()
	rb.Spec.ReleaseName = "checkout-v1.4.2"
	rb.Annotations = map[string]string{
		approval.MessageAnnotation: "Fixes the checkout timeout",
	}

	existing := testRB()
	existing.Spec.ReleaseName = "checkout-v1.4.1"

	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(existing, nil)

	k8s := testutil.NewFakeClient(prodPolicy())
	svc := &releaseBindingServiceWithAuthz{
		internal:  mockSvc,
		k8sClient: k8s,
		authz:     testutil.NewTestAuthzChecker(testutil.AllowPDP()),
	}

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)

	var list openchoreov1alpha1.ApprovalRequestList
	require.NoError(t, k8s.List(context.Background(), &list, client.InNamespace("ns-1")))
	require.Len(t, list.Items, 1, "a blocked change should open exactly one request")

	got := list.Items[0]
	require.Equal(t, "gate-dev", got.Spec.PolicyName)
	require.Equal(t, "releasebinding:update", got.Spec.Action)
	require.Equal(t, "my-rb", got.Spec.Target.Name)
	require.Equal(t, "dev", got.Spec.Target.Environment)
	require.Equal(t, openchoreov1alpha1.ApprovalPhasePending, got.Status.Phase)
	require.NotEmpty(t, got.Spec.StateFingerprint)

	// The justification travels with the change, so the approver sees why.
	require.Equal(t, "Fixes the checkout timeout", got.Spec.Message)

	// And the approver can see what would change without reading the project.
	require.NotNil(t, got.Spec.Summary)
	require.Equal(t, "checkout-v1.4.1", got.Spec.Summary.Current)
	require.Equal(t, "checkout-v1.4.2", got.Spec.Summary.Requested)
	require.Contains(t, got.Spec.Summary.Details[0], "checkout-v1.4.1 → checkout-v1.4.2")
}

// Retrying a blocked change must not pile up duplicate requests for the same
// change — the second attempt should find the first request and report it.
func TestRetryDoesNotOpenASecondRequest(t *testing.T) {
	rb := testRB()
	rb.Spec.ReleaseName = "checkout-v1.4.2"

	existing := testRB()
	existing.Spec.ReleaseName = "checkout-v1.4.1"

	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(existing, nil)

	k8s := testutil.NewFakeClient(prodPolicy())
	svc := &releaseBindingServiceWithAuthz{
		internal:  mockSvc,
		k8sClient: k8s,
		authz:     testutil.NewTestAuthzChecker(testutil.AllowPDP()),
	}

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)

	// Second attempt at the same change.
	_, err = svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.ErrorIs(t, err, approval.ErrApprovalPending)

	var list openchoreov1alpha1.ApprovalRequestList
	require.NoError(t, k8s.List(context.Background(), &list, client.InNamespace("ns-1")))
	require.Len(t, list.Items, 1, "retrying the same change should not open a second request")
}

// Changing the request after being blocked is a different change, so it needs
// its own approval rather than inheriting the pending one.
func TestDifferentChangeOpensItsOwnRequest(t *testing.T) {
	existing := testRB()
	existing.Spec.ReleaseName = "checkout-v1.4.1"

	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(existing, nil)

	k8s := testutil.NewFakeClient(prodPolicy())
	svc := &releaseBindingServiceWithAuthz{
		internal:  mockSvc,
		k8sClient: k8s,
		authz:     testutil.NewTestAuthzChecker(testutil.AllowPDP()),
	}

	first := testRB()
	first.Spec.ReleaseName = "checkout-v1.4.2"
	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", first)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)

	second := testRB()
	second.Spec.ReleaseName = "checkout-v1.4.3"
	_, err = svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", second)
	require.ErrorIs(t, err, approval.ErrApprovalRequired)

	var list openchoreov1alpha1.ApprovalRequestList
	require.NoError(t, k8s.List(context.Background(), &list, client.InNamespace("ns-1")))
	require.Len(t, list.Items, 2, "a different change needs its own request")
}

// An ungated change must not create approval objects as a side effect.
func TestUngatedUpdateOpensNoRequest(t *testing.T) {
	rb := testRB()
	mockSvc := mocks.NewMockService(t)
	mockSvc.On("GetReleaseBinding", mock.Anything, "ns-1", "my-rb").Return(rb, nil)
	mockSvc.On("UpdateReleaseBinding", mock.Anything, "ns-1", rb).Return(rb, nil)

	k8s := testutil.NewFakeClient()
	svc := &releaseBindingServiceWithAuthz{
		internal:  mockSvc,
		k8sClient: k8s,
		authz:     testutil.NewTestAuthzChecker(testutil.AllowPDP()),
	}

	_, err := svc.UpdateReleaseBinding(testutil.AuthzContext(), "ns-1", rb)
	require.NoError(t, err)

	var list openchoreov1alpha1.ApprovalRequestList
	require.NoError(t, k8s.List(context.Background(), &list, client.InNamespace("ns-1")))
	require.Empty(t, list.Items)
}
