// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalrequest

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	authz "github.com/openchoreo/openchoreo/internal/authz/core"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
)

const (
	resourceTypeApprovalRequest = "approvalrequest"
)

// approvalRequestServiceWithAuthz wraps a Service and adds authorization checks.
//
// The authz layer answers "may this subject take part in approvals here at all",
// scoped by environment through the usual CEL conditions. Whether they may
// decide *this particular* request is a separate question answered by the
// policy's approvers list, inside the service — holding approvalrequest:decide
// is necessary but not sufficient.
type approvalRequestServiceWithAuthz struct {
	internal Service
	authz    *services.AuthzChecker
}

var _ Service = (*approvalRequestServiceWithAuthz)(nil)

// NewServiceWithAuthz creates an approval request service with authorization checks.
func NewServiceWithAuthz(k8sClient client.Client, authzPDP authz.PDP, logger *slog.Logger) Service {
	return &approvalRequestServiceWithAuthz{
		internal: NewService(k8sClient, logger),
		authz:    services.NewAuthzChecker(authzPDP, logger),
	}
}

func (s *approvalRequestServiceWithAuthz) ListApprovalRequests(
	ctx context.Context, namespaceName string, filter ListFilter, opts services.ListOptions,
) (*services.ListResult[openchoreov1alpha1.ApprovalRequest], error) {
	return services.FilteredList(ctx, opts, s.authz,
		func(ctx context.Context, pageOpts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalRequest], error) {
			return s.internal.ListApprovalRequests(ctx, namespaceName, filter, pageOpts)
		},
		func(ar openchoreov1alpha1.ApprovalRequest) services.CheckRequest {
			return services.CheckRequest{
				Action:       authz.ActionViewApprovalRequest,
				ResourceType: resourceTypeApprovalRequest,
				ResourceID:   ar.Name,
				Hierarchy: authz.ResourceHierarchy{
					Namespace: namespaceName,
					Project:   ar.Spec.Target.Project,
					Component: ar.Spec.Target.Component,
				},
				Context: authz.Context{
					Resource: authz.ResourceAttribute{
						Environment: services.FormatDualScopedResourceName(namespaceName, ar.Spec.Target.Environment, false),
					},
				},
			}
		},
	)
}

func (s *approvalRequestServiceWithAuthz) GetApprovalRequest(
	ctx context.Context, namespaceName, approvalRequestName string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	ar, err := s.internal.GetApprovalRequest(ctx, namespaceName, approvalRequestName)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, namespaceName, authz.ActionViewApprovalRequest, ar); err != nil {
		return nil, err
	}
	return ar, nil
}

func (s *approvalRequestServiceWithAuthz) DecideApprovalRequest(
	ctx context.Context,
	namespaceName, approvalRequestName string,
	result openchoreov1alpha1.ApprovalResult,
	comment string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	ar, err := s.internal.GetApprovalRequest(ctx, namespaceName, approvalRequestName)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, namespaceName, authz.ActionDecideApprovalRequest, ar); err != nil {
		return nil, err
	}
	return s.internal.DecideApprovalRequest(ctx, namespaceName, approvalRequestName, result, comment)
}

func (s *approvalRequestServiceWithAuthz) CancelApprovalRequest(
	ctx context.Context, namespaceName, approvalRequestName string,
) (*openchoreov1alpha1.ApprovalRequest, error) {
	ar, err := s.internal.GetApprovalRequest(ctx, namespaceName, approvalRequestName)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, namespaceName, authz.ActionCancelApprovalRequest, ar); err != nil {
		return nil, err
	}
	return s.internal.CancelApprovalRequest(ctx, namespaceName, approvalRequestName)
}

// check runs one authorization check against the request's own hierarchy, so a
// condition like resource.environment == "production" applies to the environment
// the request targets rather than to the request object in the abstract.
func (s *approvalRequestServiceWithAuthz) check(
	ctx context.Context, namespaceName, action string, ar *openchoreov1alpha1.ApprovalRequest,
) error {
	return s.authz.Check(ctx, services.CheckRequest{
		Action:       action,
		ResourceType: resourceTypeApprovalRequest,
		ResourceID:   ar.Name,
		Hierarchy: authz.ResourceHierarchy{
			Namespace: namespaceName,
			Project:   ar.Spec.Target.Project,
			Component: ar.Spec.Target.Component,
		},
		Context: authz.Context{
			Resource: authz.ResourceAttribute{
				Environment: services.FormatDualScopedResourceName(namespaceName, ar.Spec.Target.Environment, false),
			},
		},
	})
}
