// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalrequest

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/approval"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	"github.com/openchoreo/openchoreo/internal/server/middleware/auth"
)

var approvalRequestTypeMeta = metav1.TypeMeta{
	APIVersion: openchoreov1alpha1.GroupVersion.String(),
	Kind:       "ApprovalRequest",
}

// approvalRequestService handles approval request business logic without
// authorization checks. Approver eligibility is not an authorization check in
// the usual sense — it is the policy's own approvers list — so it is enforced
// here rather than in the authz wrapper.
type approvalRequestService struct {
	k8sClient client.Client
	logger    *slog.Logger
}

var _ Service = (*approvalRequestService)(nil)

// NewService creates a new approval request service without authorization.
func NewService(k8sClient client.Client, logger *slog.Logger) Service {
	return &approvalRequestService{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

func (s *approvalRequestService) ListApprovalRequests(
	ctx context.Context, namespaceName string, filter ListFilter, opts services.ListOptions,
) (*services.ListResult[openchoreov1alpha1.ApprovalRequest], error) {
	commonOpts, err := services.BuildListOptions(opts)
	if err != nil {
		return nil, err
	}
	listOpts := append([]client.ListOption{client.InNamespace(namespaceName)}, commonOpts...)

	var arList openchoreov1alpha1.ApprovalRequestList
	if err := s.k8sClient.List(ctx, &arList, listOpts...); err != nil {
		s.logger.Error("Failed to list approval requests", "error", err)
		return nil, fmt.Errorf("failed to list approval requests: %w", err)
	}

	// Phase and environment live in status and a nested spec field respectively,
	// neither of which is a label, so filtering happens here rather than through a
	// selector the API server could apply.
	items := make([]openchoreov1alpha1.ApprovalRequest, 0, len(arList.Items))
	for i := range arList.Items {
		ar := arList.Items[i]
		if filter.Phase != "" && string(ar.Status.Phase) != filter.Phase {
			continue
		}
		if filter.Environment != "" && ar.Spec.Target.Environment != filter.Environment {
			continue
		}
		ar.TypeMeta = approvalRequestTypeMeta
		items = append(items, ar)
	}

	result := &services.ListResult[openchoreov1alpha1.ApprovalRequest]{
		Items:      items,
		NextCursor: arList.Continue,
	}
	if arList.RemainingItemCount != nil {
		remaining := *arList.RemainingItemCount
		result.RemainingCount = &remaining
	}
	return result, nil
}

func (s *approvalRequestService) GetApprovalRequest(
	ctx context.Context, namespaceName, approvalRequestName string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	ar := &openchoreov1alpha1.ApprovalRequest{}
	key := client.ObjectKey{Name: approvalRequestName, Namespace: namespaceName}

	if err := s.k8sClient.Get(ctx, key, ar); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, ErrApprovalRequestNotFound
		}
		s.logger.Error("Failed to get approval request", "error", err)
		return nil, fmt.Errorf("failed to get approval request: %w", err)
	}

	ar.TypeMeta = approvalRequestTypeMeta
	return ar, nil
}

// DecideApprovalRequest records a decision, after checking that the caller is
// actually entitled to make it.
func (s *approvalRequestService) DecideApprovalRequest(
	ctx context.Context,
	namespaceName, approvalRequestName string,
	result openchoreov1alpha1.ApprovalResult,
	comment string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	if result == openchoreov1alpha1.ApprovalResultRejected && comment == "" {
		return nil, ErrCommentRequired
	}

	ar, err := s.GetApprovalRequest(ctx, namespaceName, approvalRequestName)
	if err != nil {
		return nil, err
	}
	if ar.Status.Phase != openchoreov1alpha1.ApprovalPhasePending {
		return nil, fmt.Errorf("%w: request is %s", ErrNotPending, ar.Status.Phase)
	}

	policy := &openchoreov1alpha1.ApprovalPolicy{}
	policyKey := client.ObjectKey{Name: ar.Spec.PolicyName, Namespace: namespaceName}
	if err := s.k8sClient.Get(ctx, policyKey, policy); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, ErrPolicyNotFound
		}
		return nil, fmt.Errorf("failed to get approval policy: %w", err)
	}

	subject, claims := subjectAndClaims(ctx)

	// Role-based approvers are resolved through the subject's entitlements, which
	// is the same claim material the PDP evaluates roles from. A dedicated PDP
	// call per approver role would be better and is deliberately deferred: it
	// needs a resource identity to evaluate against, which a request about a
	// binding in another project does not straightforwardly provide.
	allowed, reason := approval.CanDecide(policy, ar, subject, claims, false)
	if !allowed {
		s.logger.Warn("Approval decision refused",
			"request", approvalRequestName, "subject", subject.ID, "reason", reason)
		if subject.ID == ar.Spec.Requester.ID && !policy.Spec.AllowSelfApproval {
			return nil, ErrSelfApproval
		}
		return nil, ErrNotAnApprover
	}

	now := metav1.Now()
	ar.Spec.Decision = &openchoreov1alpha1.ApprovalDecision{
		Result:    result,
		DecidedBy: subject,
		DecidedAt: now,
		Comment:   comment,
	}
	if err := s.k8sClient.Update(ctx, ar); err != nil {
		if vErr := services.ExtractValidationError(err); vErr != nil {
			return nil, vErr
		}
		return nil, fmt.Errorf("failed to record decision: %w", err)
	}

	// The phase is written here as well as by the controller so that the response
	// to the approver reflects their decision immediately, rather than showing
	// Pending until a reconcile catches up.
	if result == openchoreov1alpha1.ApprovalResultApproved {
		ar.Status.Phase = openchoreov1alpha1.ApprovalPhaseApproved
	} else {
		ar.Status.Phase = openchoreov1alpha1.ApprovalPhaseRejected
		ar.Status.CompletedAt = &now
	}
	if err := s.k8sClient.Status().Update(ctx, ar); err != nil {
		return nil, fmt.Errorf("failed to update approval request status: %w", err)
	}

	ar.TypeMeta = approvalRequestTypeMeta
	return ar, nil
}

// CancelApprovalRequest withdraws a pending request.
func (s *approvalRequestService) CancelApprovalRequest(
	ctx context.Context, namespaceName, approvalRequestName string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	ar, err := s.GetApprovalRequest(ctx, namespaceName, approvalRequestName)
	if err != nil {
		return nil, err
	}
	if ar.Status.Phase != openchoreov1alpha1.ApprovalPhasePending {
		return nil, fmt.Errorf("%w: request is %s", ErrNotPending, ar.Status.Phase)
	}

	subject, _ := subjectAndClaims(ctx)
	if subject.ID != ar.Spec.Requester.ID {
		return nil, ErrNotRequester
	}

	now := metav1.Now()
	ar.Status.Phase = openchoreov1alpha1.ApprovalPhaseCancelled
	ar.Status.CompletedAt = &now
	if err := s.k8sClient.Status().Update(ctx, ar); err != nil {
		return nil, fmt.Errorf("failed to cancel approval request: %w", err)
	}

	ar.TypeMeta = approvalRequestTypeMeta
	return ar, nil
}

// subjectAndClaims resolves who is acting and the entitlement claims they carry,
// which is all the identity material available: there is no user directory to
// look anything else up in.
func subjectAndClaims(ctx context.Context) (openchoreov1alpha1.ApprovalSubject, map[string][]string) {
	sc, ok := auth.GetSubjectContextFromContext(ctx)
	if !ok || sc == nil {
		return openchoreov1alpha1.ApprovalSubject{}, nil
	}
	subject := openchoreov1alpha1.ApprovalSubject{ID: sc.ID, DisplayName: sc.ID}
	claims := map[string][]string{}
	if sc.EntitlementClaim != "" {
		claims[sc.EntitlementClaim] = sc.EntitlementValues
	}
	return subject, claims
}
