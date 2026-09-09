// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

// ListParams defines parameters for listing approval requests.
type ListParams struct {
	Namespace   string
	Phase       string
	Environment string
}

func (p ListParams) GetNamespace() string { return p.Namespace }

// GetParams defines parameters for getting a single approval request.
type GetParams struct {
	Namespace           string
	ApprovalRequestName string
}

func (p GetParams) GetNamespace() string { return p.Namespace }

// DecideParams defines parameters for approving or rejecting a request.
type DecideParams struct {
	Namespace           string
	ApprovalRequestName string
	Comment             string
}

func (p DecideParams) GetNamespace() string { return p.Namespace }

// CancelParams defines parameters for withdrawing a request.
type CancelParams struct {
	Namespace           string
	ApprovalRequestName string
}

func (p CancelParams) GetNamespace() string { return p.Namespace }

// PolicyListParams defines parameters for listing approval policies.
type PolicyListParams struct {
	Namespace string
}

func (p PolicyListParams) GetNamespace() string { return p.Namespace }

// PolicyGetParams defines parameters for getting a single approval policy.
type PolicyGetParams struct {
	Namespace          string
	ApprovalPolicyName string
}

func (p PolicyGetParams) GetNamespace() string { return p.Namespace }

// PolicyDeleteParams defines parameters for deleting an approval policy.
type PolicyDeleteParams struct {
	Namespace          string
	ApprovalPolicyName string
}

func (p PolicyDeleteParams) GetNamespace() string { return p.Namespace }
