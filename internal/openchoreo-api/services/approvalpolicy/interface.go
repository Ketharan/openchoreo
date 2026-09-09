// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalpolicy

import (
	"context"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
)

// Service defines the approval policy service interface.
type Service interface {
	CreateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error)
	UpdateApprovalPolicy(ctx context.Context, namespaceName string, ap *openchoreov1alpha1.ApprovalPolicy) (*openchoreov1alpha1.ApprovalPolicy, error)
	ListApprovalPolicies(ctx context.Context, namespaceName string, opts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ApprovalPolicy], error)
	GetApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) (*openchoreov1alpha1.ApprovalPolicy, error)
	DeleteApprovalPolicy(ctx context.Context, namespaceName, approvalPolicyName string) error
}
