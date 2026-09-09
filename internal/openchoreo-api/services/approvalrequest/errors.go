// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approvalrequest

import "errors"

var (
	ErrApprovalRequestNotFound = errors.New("approval request not found")

	// ErrNotPending is returned when a decision or cancellation is attempted on a
	// request that has already reached a terminal phase. A decided request stays
	// decided: a second attempt opens a new request rather than overwriting the
	// history of the first.
	ErrNotPending = errors.New("approval request is not pending")

	// ErrNotAnApprover is returned when the caller is not named by the policy.
	ErrNotAnApprover = errors.New("not an approver for this request")

	// ErrSelfApproval is returned when the requester tries to decide their own
	// request under a policy that does not permit it.
	ErrSelfApproval = errors.New("self-approval is not permitted by this policy")

	// ErrCommentRequired is returned when a rejection carries no comment. A
	// rejection with no reason is the worst outcome for the requester.
	ErrCommentRequired = errors.New("a comment is required when rejecting")

	// ErrNotRequester is returned when someone other than the requester tries to
	// cancel a request.
	ErrNotRequester = errors.New("only the requester may cancel this request")

	ErrPolicyNotFound = errors.New("the policy that gated this request no longer exists")
)
