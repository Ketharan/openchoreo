// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/openchoreo/openchoreo/internal/occ/resources/client/mocks"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/api/gen"
)

func strPtr(s string) *string { return &s }

// Rejecting without a reason must fail locally, before any call is made: the
// server enforces this too, but a round trip to be told off is a worse
// experience than being told immediately.
func TestRejectRequiresComment(t *testing.T) {
	cl := mocks.NewMockInterface(t)
	// No call is expected on the client; mockery fails the test if one is made.

	err := New(cl).Reject(DecideParams{
		Namespace:           "acme",
		ApprovalRequestName: "apr-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "comment is required")
}

func TestRejectWithWhitespaceOnlyCommentIsRefused(t *testing.T) {
	cl := mocks.NewMockInterface(t)

	err := New(cl).Reject(DecideParams{
		Namespace:           "acme",
		ApprovalRequestName: "apr-1",
		Comment:             "   ",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "comment is required")
}

func TestApproveSendsApprovedResult(t *testing.T) {
	cl := mocks.NewMockInterface(t)
	cl.On("DecideApprovalRequest", mock.Anything, "acme", "apr-1",
		mock.MatchedBy(func(body gen.ApprovalDecisionRequest) bool {
			return body.Result == gen.ApprovalDecisionRequestResultApproved &&
				body.Comment != nil && *body.Comment == "soak looks clean"
		})).
		Return(&gen.ApprovalRequest{
			Metadata: gen.ObjectMeta{Name: "apr-1"},
		}, nil)

	err := New(cl).Approve(DecideParams{
		Namespace:           "acme",
		ApprovalRequestName: "apr-1",
		Comment:             "soak looks clean",
	})
	require.NoError(t, err)
}

func TestRejectSendsRejectedResult(t *testing.T) {
	cl := mocks.NewMockInterface(t)
	cl.On("DecideApprovalRequest", mock.Anything, "acme", "apr-1",
		mock.MatchedBy(func(body gen.ApprovalDecisionRequest) bool {
			return body.Result == gen.ApprovalDecisionRequestResultRejected
		})).
		Return(&gen.ApprovalRequest{
			Metadata: gen.ObjectMeta{Name: "apr-1"},
		}, nil)

	err := New(cl).Reject(DecideParams{
		Namespace:           "acme",
		ApprovalRequestName: "apr-1",
		Comment:             "wait for the incident to close",
	})
	require.NoError(t, err)
}

func TestListPassesPhaseAndEnvironmentFilters(t *testing.T) {
	cl := mocks.NewMockInterface(t)
	cl.On("ListApprovalRequests", mock.Anything, "acme",
		mock.MatchedBy(func(p *gen.ListApprovalRequestsParams) bool {
			return p != nil &&
				p.Phase != nil && string(*p.Phase) == "Pending" &&
				p.Environment != nil && *p.Environment == "production"
		})).
		Return(&gen.ApprovalRequestList{
			Items:      []gen.ApprovalRequest{},
			Pagination: gen.Pagination{},
		}, nil)

	err := New(cl).List(ListParams{
		Namespace:   "acme",
		Phase:       "Pending",
		Environment: "production",
	})
	require.NoError(t, err)
}

// A missing namespace is caught before any request, so the user gets a usage
// error rather than a confusing server-side failure.
func TestListRequiresNamespace(t *testing.T) {
	cl := mocks.NewMockInterface(t)

	err := New(cl).List(ListParams{})
	require.Error(t, err)
}

func TestCancelRequiresName(t *testing.T) {
	cl := mocks.NewMockInterface(t)

	err := New(cl).Cancel(CancelParams{Namespace: "acme"})
	require.Error(t, err)
}

// The request table must stay readable when a request carries no summary yet,
// which is the state every request is in the moment it is created.
func TestPrintRequestListToleratesSparseRequests(t *testing.T) {
	err := printRequestList([]gen.ApprovalRequest{
		{
			Metadata: gen.ObjectMeta{Name: "apr-sparse"},
			Spec: &gen.ApprovalRequestSpec{
				PolicyName: "p",
				Action:     "releasebinding:update",
				Target:     gen.ApprovalTarget{Kind: "ReleaseBinding", Name: "rb"},
				Requester:  gen.ApprovalSubject{Id: "dev-1"},
			},
		},
		{
			Metadata: gen.ObjectMeta{Name: "apr-full"},
			Spec: &gen.ApprovalRequestSpec{
				PolicyName: "p",
				Action:     "releasebinding:update",
				Target: gen.ApprovalTarget{
					Kind: "ReleaseBinding", Name: "rb",
					Component:   strPtr("checkout-svc"),
					Environment: strPtr("production"),
				},
				Requester: gen.ApprovalSubject{Id: "dev-1", DisplayName: strPtr("Ketharan")},
				Summary:   &gen.ApprovalSummary{Requested: strPtr("checkout-v1.4.2")},
			},
		},
	})
	require.NoError(t, err)
}

func TestPrintPolicyListToleratesSparsePolicies(t *testing.T) {
	err := printPolicyList([]gen.ApprovalPolicy{
		{Metadata: gen.ObjectMeta{Name: "no-spec"}},
	})
	require.NoError(t, err)
}
