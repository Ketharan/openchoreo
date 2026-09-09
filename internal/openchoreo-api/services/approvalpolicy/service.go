// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalpolicy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	authz "github.com/openchoreo/openchoreo/internal/authz/core"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
)

var approvalPolicyTypeMeta = metav1.TypeMeta{
	APIVersion: openchoreov1alpha1.GroupVersion.String(),
	Kind:       "ApprovalPolicy",
}

// approvalPolicyService handles approval policy business logic without authorization checks.
type approvalPolicyService struct {
	k8sClient client.Client
	logger    *slog.Logger
}

var _ Service = (*approvalPolicyService)(nil)

// NewService creates a new approval policy service without authorization.
func NewService(k8sClient client.Client, logger *slog.Logger) Service {
	return &approvalPolicyService{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// validateAction rejects a policy naming an action with no enforcement point,
// so the failure surfaces when the policy is written rather than as silence at
// the moment somebody expected to be stopped.
func validateAction(action string) error {
	if authz.IsApprovable(action) {
		return nil
	}
	return fmt.Errorf("%w: %q is not approvable; gatable actions are: %s",
		ErrActionNotApprovable, action, strings.Join(authz.ApprovableActions(), ", "))
}

func (s *approvalPolicyService) CreateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error) {
	if ap == nil {
		return nil, fmt.Errorf("approval policy cannot be nil")
	}
	if err := validateAction(ap.Spec.Action); err != nil {
		return nil, err
	}

	s.logger.Debug("Creating approval policy", "namespace", namespaceName, "approvalPolicy", ap.Name)

	ap.Status = openchoreov1alpha1.ApprovalPolicyStatus{}
	ap.Namespace = namespaceName
	if err := s.k8sClient.Create(ctx, ap); err != nil {
		if apierrors.IsAlreadyExists(err) {
			s.logger.Warn("Approval policy already exists", "namespace", namespaceName, "approvalPolicy", ap.Name)
			return nil, ErrApprovalPolicyAlreadyExists
		}
		if vErr := services.ExtractValidationError(err); vErr != nil {
			return nil, vErr
		}
		s.logger.Error("Failed to create approval policy CR", "error", err)
		return nil, fmt.Errorf("failed to create approval policy: %w", err)
	}

	ap.TypeMeta = approvalPolicyTypeMeta
	return ap, nil
}

func (s *approvalPolicyService) UpdateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error) {
	if ap == nil {
		return nil, fmt.Errorf("approval policy cannot be nil")
	}
	if err := validateAction(ap.Spec.Action); err != nil {
		return nil, err
	}

	s.logger.Debug("Updating approval policy", "namespace", namespaceName, "approvalPolicy", ap.Name)

	existing := &openchoreov1alpha1.ApprovalPolicy{}
	if err := s.k8sClient.Get(ctx, client.ObjectKey{Name: ap.Name, Namespace: namespaceName}, existing); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, ErrApprovalPolicyNotFound
		}
		s.logger.Error("Failed to get approval policy", "error", err)
		return nil, fmt.Errorf("failed to get approval policy: %w", err)
	}

	ap.Status = openchoreov1alpha1.ApprovalPolicyStatus{}
	existing.Spec = ap.Spec
	existing.Labels = ap.Labels
	existing.Annotations = ap.Annotations

	if err := s.k8sClient.Update(ctx, existing); err != nil {
		if vErr := services.ExtractValidationError(err); vErr != nil {
			return nil, vErr
		}
		s.logger.Error("Failed to update approval policy CR", "error", err)
		return nil, fmt.Errorf("failed to update approval policy: %w", err)
	}

	existing.TypeMeta = approvalPolicyTypeMeta
	return existing, nil
}

func (s *approvalPolicyService) ListApprovalPolicies(ctx context.Context, namespaceName string, opts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalPolicy], error) {
	commonOpts, err := services.BuildListOptions(opts)
	if err != nil {
		return nil, err
	}
	listOpts := append([]client.ListOption{client.InNamespace(namespaceName)}, commonOpts...)

	var apList openchoreov1alpha1.ApprovalPolicyList
	if err := s.k8sClient.List(ctx, &apList, listOpts...); err != nil {
		s.logger.Error("Failed to list approval policies", "error", err)
		return nil, fmt.Errorf("failed to list approval policies: %w", err)
	}

	for i := range apList.Items {
		apList.Items[i].TypeMeta = approvalPolicyTypeMeta
	}

	result := &services.ListResult[openchoreov1alpha1.ApprovalPolicy]{
		Items:      apList.Items,
		NextCursor: apList.Continue,
	}
	if apList.RemainingItemCount != nil {
		remaining := *apList.RemainingItemCount
		result.RemainingCount = &remaining
	}
	return result, nil
}

func (s *approvalPolicyService) GetApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) (*openchoreov1alpha1.ApprovalPolicy, error) {
	ap := &openchoreov1alpha1.ApprovalPolicy{}
	key := client.ObjectKey{Name: approvalPolicyName, Namespace: namespaceName}

	if err := s.k8sClient.Get(ctx, key, ap); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, ErrApprovalPolicyNotFound
		}
		s.logger.Error("Failed to get approval policy", "error", err)
		return nil, fmt.Errorf("failed to get approval policy: %w", err)
	}

	ap.TypeMeta = approvalPolicyTypeMeta
	return ap, nil
}

func (s *approvalPolicyService) DeleteApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) error {
	ap := &openchoreov1alpha1.ApprovalPolicy{}
	ap.Name = approvalPolicyName
	ap.Namespace = namespaceName

	if err := s.k8sClient.Delete(ctx, ap); err != nil {
		if apierrors.IsNotFound(err) {
			return ErrApprovalPolicyNotFound
		}
		s.logger.Error("Failed to delete approval policy", "error", err)
		return fmt.Errorf("failed to delete approval policy: %w", err)
	}
	return nil
}
