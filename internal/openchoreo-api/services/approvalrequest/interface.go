// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalrequest

import (
	"context"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
)

// ListFilter narrows an approval request listing.
type ListFilter struct {
	// Phase restricts results to one lifecycle phase. Empty means all.
	Phase string
	// Environment restricts results to requests targeting one environment.
	Environment string
}

// Service defines the approval request service interface.
type Service interface {
	ListApprovalRequests(ctx context.Context, namespaceName string, filter ListFilter, opts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalRequest], error)
	GetApprovalRequest(ctx context.Context, namespaceName, approvalRequestName string) (*openchoreov1alpha1.ApprovalRequest, error)

	// DecideApprovalRequest records an approve or reject on a pending request.
	DecideApprovalRequest(ctx context.Context, namespaceName, approvalRequestName string, result openchoreov1alpha1.ApprovalResult, comment string) (*openchoreov1alpha1.ApprovalRequest, error)

	// CancelApprovalRequest withdraws a pending request.
	CancelApprovalRequest(ctx context.Context, namespaceName, approvalRequestName string) (*openchoreov1alpha1.ApprovalRequest, error)
}
