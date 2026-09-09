// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalpolicy

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	authz "github.com/openchoreo/openchoreo/internal/authz/core"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
)

const (
	resourceTypeApprovalPolicy = "approvalpolicy"
)

// approvalPolicyServiceWithAuthz wraps a Service and adds authorization checks.
// Handlers should use this. Other services should use the unwrapped Service directly.
//
// Note that these actions govern the gate itself: whoever holds
// approvalpolicy:update can suspend a policy and promote freely. The permission
// is therefore as sensitive as the promotion it protects, and default roles
// should not hand it to the same people the gate is meant to hold.
type approvalPolicyServiceWithAuthz struct {
	internal Service
	authz    *services.AuthzChecker
}

var _ Service = (*approvalPolicyServiceWithAuthz)(nil)

// NewServiceWithAuthz creates an approval policy service with authorization checks.
func NewServiceWithAuthz(k8sClient client.Client, authzPDP authz.PDP, logger *slog.Logger) Service {
	return &approvalPolicyServiceWithAuthz{
		internal: NewService(k8sClient, logger),
		authz:    services.NewAuthzChecker(authzPDP, logger),
	}
}

func (s *approvalPolicyServiceWithAuthz) CreateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error) {
	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionCreateApprovalPolicy,
		ResourceType: resourceTypeApprovalPolicy,
		ResourceID:   ap.Name,
		Hierarchy:    authz.ResourceHierarchy{Namespace: namespaceName},
	}); err != nil {
		return nil, err
	}
	return s.internal.CreateApprovalPolicy(ctx, namespaceName, ap)
}

func (s *approvalPolicyServiceWithAuthz) UpdateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error) {
	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionUpdateApprovalPolicy,
		ResourceType: resourceTypeApprovalPolicy,
		ResourceID:   ap.Name,
		Hierarchy:    authz.ResourceHierarchy{Namespace: namespaceName},
	}); err != nil {
		return nil, err
	}
	return s.internal.UpdateApprovalPolicy(ctx, namespaceName, ap)
}

func (s *approvalPolicyServiceWithAuthz) ListApprovalPolicies(ctx context.Context, namespaceName string, opts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalPolicy], error) {
	return services.FilteredList(ctx, opts, s.authz,
		func(ctx context.Context, pageOpts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalPolicy], error) {
			return s.internal.ListApprovalPolicies(ctx, namespaceName, pageOpts)
		},
		func(ap openchoreov1alpha1.ApprovalPolicy) services.CheckRequest {
			return services.CheckRequest{
				Action:       authz.ActionViewApprovalPolicy,
				ResourceType: resourceTypeApprovalPolicy,
				ResourceID:   ap.Name,
				Hierarchy:    authz.ResourceHierarchy{Namespace: namespaceName},
			}
		},
	)
}

func (s *approvalPolicyServiceWithAuthz) GetApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) (*openchoreov1alpha1.ApprovalPolicy, error) {
	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionViewApprovalPolicy,
		ResourceType: resourceTypeApprovalPolicy,
		ResourceID:   approvalPolicyName,
		Hierarchy:    authz.ResourceHierarchy{Namespace: namespaceName},
	}); err != nil {
		return nil, err
	}
	return s.internal.GetApprovalPolicy(ctx, namespaceName, approvalPolicyName)
}

func (s *approvalPolicyServiceWithAuthz) DeleteApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) error {
	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionDeleteApprovalPolicy,
		ResourceType: resourceTypeApprovalPolicy,
		ResourceID:   approvalPolicyName,
		Hierarchy:    authz.ResourceHierarchy{Namespace: namespaceName},
	}); err != nil {
		return err
	}
	return s.internal.DeleteApprovalPolicy(ctx, namespaceName, approvalPolicyName)
}
