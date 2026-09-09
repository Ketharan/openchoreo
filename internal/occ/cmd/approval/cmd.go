// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"github.com/spf13/cobra"

	"github.com/openchoreo/openchoreo/internal/occ/auth"
	"github.com/openchoreo/openchoreo/internal/occ/cmdutil"
	"github.com/openchoreo/openchoreo/internal/occ/flags"
	"github.com/openchoreo/openchoreo/internal/occ/resources/client"
)

// NewApprovalCmd builds the `occ approval` command tree.
func NewApprovalCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "approval",
		Aliases: []string{"approvals", "apreq"},
		Short:   "Review and decide approval requests",
		Long: `Review and decide approval requests for OpenChoreo.

An approval request is raised when a policy gates an action, such as promoting a
component into a protected environment. The action does not take effect until an
approver decides.`,
	}
	cmd.AddCommand(
		newListCmd(f),
		newGetCmd(f),
		newApproveCmd(f),
		newRejectCmd(f),
		newCancelCmd(f),
		newPolicyCmd(f),
	)
	return cmd
}

func newListCmd(f client.NewClientFunc) *cobra.Command {
	var phase, environment string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approval requests",
		Long:  `List approval requests in a namespace, optionally filtered by phase or environment.`,
		Example: `  # Everything awaiting a decision
  occ approval list --namespace acme-corp --phase Pending

  # Only production requests
  occ approval list --namespace acme-corp --environment production`,
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).List(ListParams{
				Namespace:   flags.GetNamespace(cmd),
				Phase:       phase,
				Environment: environment,
			})
		},
	}
	flags.AddNamespace(cmd)
	cmd.Flags().StringVar(&phase, "phase", "",
		"Filter by phase (Pending, Approved, Rejected, Cancelled, Executed, Failed)")
	cmd.Flags().StringVar(&environment, "environment", "", "Filter by target environment")
	return cmd
}

func newGetCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [APPROVAL_REQUEST_NAME]",
		Short: "Get an approval request",
		Long: `Get an approval request in YAML format, including the summary of what would
change, so a decision can be made without inspecting the target resource.`,
		Example: `  # Review what is being asked for
  occ approval get apr-7f3c9a --namespace acme-corp`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).Get(GetParams{
				Namespace:           flags.GetNamespace(cmd),
				ApprovalRequestName: args[0],
			})
		},
	}
	flags.AddNamespace(cmd)
	return cmd
}

func newApproveCmd(f client.NewClientFunc) *cobra.Command {
	var comment string

	cmd := &cobra.Command{
		Use:   "approve [APPROVAL_REQUEST_NAME]",
		Short: "Approve a pending request",
		Long: `Approve a pending request.

The approval covers exactly the change described by the request. If the change is
altered afterwards, a new request is required.`,
		Example: `  # Approve, saying why
  occ approval approve apr-7f3c9a --namespace acme-corp --comment "Staging soak looks clean"`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).Approve(DecideParams{
				Namespace:           flags.GetNamespace(cmd),
				ApprovalRequestName: args[0],
				Comment:             comment,
			})
		},
	}
	flags.AddNamespace(cmd)
	cmd.Flags().StringVar(&comment, "comment", "", "Comment shown to the requester")
	return cmd
}

func newRejectCmd(f client.NewClientFunc) *cobra.Command {
	var comment string

	cmd := &cobra.Command{
		Use:   "reject [APPROVAL_REQUEST_NAME]",
		Short: "Reject a pending request",
		Long: `Reject a pending request.

A comment is required: the requester needs to know what to change before asking
again. Rejection is final — a further attempt raises a new request, so both
decisions remain in the history.`,
		Example: `  # Reject with a reason
  occ approval reject apr-7f3c9a --namespace acme-corp --comment "Wait for the incident to close"`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).Reject(DecideParams{
				Namespace:           flags.GetNamespace(cmd),
				ApprovalRequestName: args[0],
				Comment:             comment,
			})
		},
	}
	flags.AddNamespace(cmd)
	cmd.Flags().StringVar(&comment, "comment", "", "Reason for rejecting (required)")
	return cmd
}

func newCancelCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel [APPROVAL_REQUEST_NAME]",
		Short: "Withdraw your own pending request",
		Long: `Withdraw a pending request you raised, so nobody reviews a change that is no
longer wanted.`,
		Example: `  # Withdraw a request
  occ approval cancel apr-7f3c9a --namespace acme-corp`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).Cancel(CancelParams{
				Namespace:           flags.GetNamespace(cmd),
				ApprovalRequestName: args[0],
			})
		},
	}
	flags.AddNamespace(cmd)
	return cmd
}

func newPolicyCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "policy",
		Aliases: []string{"policies"},
		Short:   "Inspect approval policies",
		Long: `Inspect the policies that decide which actions require approval.

Policies are created and edited by platform engineers, normally through applied
manifests rather than here.`,
	}
	cmd.AddCommand(
		newPolicyListCmd(f),
		newPolicyGetCmd(f),
		newPolicyDeleteCmd(f),
	)
	return cmd
}

func newPolicyListCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approval policies",
		Long:  `List the approval policies in a namespace.`,
		Example: `  # See what is gated
  occ approval policy list --namespace acme-corp`,
		PreRunE: auth.RequireLogin(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).ListPolicies(PolicyListParams{
				Namespace: flags.GetNamespace(cmd),
			})
		},
	}
	flags.AddNamespace(cmd)
	return cmd
}

func newPolicyGetCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "get [APPROVAL_POLICY_NAME]",
		Short:   "Get an approval policy",
		Long:    `Get an approval policy in YAML format.`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		Example: `  # Inspect a policy
  occ approval policy get production-changes --namespace acme-corp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).GetPolicy(PolicyGetParams{
				Namespace:          flags.GetNamespace(cmd),
				ApprovalPolicyName: args[0],
			})
		},
	}
	flags.AddNamespace(cmd)
	return cmd
}

func newPolicyDeleteCmd(f client.NewClientFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [APPROVAL_POLICY_NAME]",
		Short: "Delete an approval policy",
		Long: `Delete an approval policy.

Deleting a policy removes the gate. To stop gating temporarily while keeping the
policy and its history, set spec.suspend on the policy instead.`,
		Args:    cmdutil.ExactOneArgWithUsage(),
		PreRunE: auth.RequireLogin(),
		Example: `  # Remove a policy
  occ approval policy delete production-changes --namespace acme-corp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := f()
			if err != nil {
				return err
			}
			return New(cl).DeletePolicy(PolicyDeleteParams{
				Namespace:          flags.GetNamespace(cmd),
				ApprovalPolicyName: args[0],
			})
		},
	}
	flags.AddNamespace(cmd)
	return cmd
}
