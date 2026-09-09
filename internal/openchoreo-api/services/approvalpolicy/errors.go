// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalpolicy

import "errors"

var (
	ErrApprovalPolicyNotFound      = errors.New("approval policy not found")
	ErrApprovalPolicyAlreadyExists = errors.New("approval policy already exists")

	// ErrActionNotApprovable is returned when a policy names an action that has no
	// enforcement point. Rejecting this at write time is the whole reason the
	// action registry exists: a policy that cannot be enforced would look correct
	// and never fire, which is the failure the removed DeploymentPipeline approval
	// flags had.
	ErrActionNotApprovable = errors.New("action cannot be gated by an approval policy")
)
