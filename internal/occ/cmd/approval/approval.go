// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package approval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"sigs.k8s.io/yaml"

	"github.com/openchoreo/openchoreo/internal/occ/cmd/pagination"
	"github.com/openchoreo/openchoreo/internal/occ/cmd/utils"
	"github.com/openchoreo/openchoreo/internal/occ/cmdutil"
	"github.com/openchoreo/openchoreo/internal/occ/resources/client"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/api/gen"
)

// Approval implements approval request and policy operations.
type Approval struct {
	client client.Interface
}

// New creates a new approval implementation.
func New(c client.Interface) *Approval {
	return &Approval{client: c}
}

// List lists approval requests, optionally narrowed by phase or environment.
func (a *Approval) List(params ListParams) error {
	if err := cmdutil.RequireFields("list", "approval", map[string]string{"namespace": params.Namespace}); err != nil {
		return err
	}

	ctx := context.Background()

	items, err := pagination.FetchAll(func(limit int, cursor string) ([]gen.ApprovalRequest, string, error) {
		p := &gen.ListApprovalRequestsParams{}
		p.Limit = &limit
		if cursor != "" {
			p.Cursor = &cursor
		}
		if params.Phase != "" {
			phase := gen.ListApprovalRequestsParamsPhase(params.Phase)
			p.Phase = &phase
		}
		if params.Environment != "" {
			p.Environment = &params.Environment
		}
		result, err := a.client.ListApprovalRequests(ctx, params.Namespace, p)
		if err != nil {
			return nil, "", err
		}
		next := ""
		if result.Pagination.NextCursor != nil {
			next = *result.Pagination.NextCursor
		}
		return result.Items, next, nil
	})
	if err != nil {
		return err
	}
	return printRequestList(items)
}

// Get retrieves a single approval request as YAML.
func (a *Approval) Get(params GetParams) error {
	if err := cmdutil.RequireFields("get", "approval", map[string]string{
		"namespace": params.Namespace, "name": params.ApprovalRequestName,
	}); err != nil {
		return err
	}

	result, err := a.client.GetApprovalRequest(context.Background(), params.Namespace, params.ApprovalRequestName)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal approval request to YAML: %w", err)
	}
	fmt.Print(string(data))
	return nil
}

// Approve records an approval on a pending request.
func (a *Approval) Approve(params DecideParams) error {
	return a.decide(params, gen.ApprovalDecisionRequestResultApproved)
}

// Reject records a rejection. The comment is required here rather than
// optional: a rejection with no reason leaves the requester with nothing to act
// on, which is the worst outcome the flow can produce.
func (a *Approval) Reject(params DecideParams) error {
	if strings.TrimSpace(params.Comment) == "" {
		return fmt.Errorf("a comment is required when rejecting: use --comment to say why")
	}
	return a.decide(params, gen.ApprovalDecisionRequestResultRejected)
}

func (a *Approval) decide(params DecideParams, result gen.ApprovalDecisionRequestResult) error {
	if err := cmdutil.RequireFields("decide", "approval", map[string]string{
		"namespace": params.Namespace, "name": params.ApprovalRequestName,
	}); err != nil {
		return err
	}

	body := gen.ApprovalDecisionRequest{Result: result}
	if params.Comment != "" {
		body.Comment = &params.Comment
	}

	updated, err := a.client.DecideApprovalRequest(
		context.Background(), params.Namespace, params.ApprovalRequestName, body)
	if err != nil {
		return err
	}

	verb := "Approved"
	if result == gen.ApprovalDecisionRequestResultRejected {
		verb = "Rejected"
	}
	fmt.Printf("%s %s.\n", verb, updated.Metadata.Name)
	return nil
}

// Cancel withdraws a pending request.
func (a *Approval) Cancel(params CancelParams) error {
	if err := cmdutil.RequireFields("cancel", "approval", map[string]string{
		"namespace": params.Namespace, "name": params.ApprovalRequestName,
	}); err != nil {
		return err
	}

	updated, err := a.client.CancelApprovalRequest(
		context.Background(), params.Namespace, params.ApprovalRequestName)
	if err != nil {
		return err
	}

	fmt.Printf("Cancelled %s.\n", updated.Metadata.Name)
	return nil
}

// ListPolicies lists approval policies in a namespace.
func (a *Approval) ListPolicies(params PolicyListParams) error {
	if err := cmdutil.RequireFields("list", "approvalpolicy", map[string]string{"namespace": params.Namespace}); err != nil {
		return err
	}

	ctx := context.Background()

	items, err := pagination.FetchAll(func(limit int, cursor string) ([]gen.ApprovalPolicy, string, error) {
		p := &gen.ListApprovalPoliciesParams{}
		p.Limit = &limit
		if cursor != "" {
			p.Cursor = &cursor
		}
		result, err := a.client.ListApprovalPolicies(ctx, params.Namespace, p)
		if err != nil {
			return nil, "", err
		}
		next := ""
		if result.Pagination.NextCursor != nil {
			next = *result.Pagination.NextCursor
		}
		return result.Items, next, nil
	})
	if err != nil {
		return err
	}
	return printPolicyList(items)
}

// GetPolicy retrieves a single approval policy as YAML.
func (a *Approval) GetPolicy(params PolicyGetParams) error {
	if err := cmdutil.RequireFields("get", "approvalpolicy", map[string]string{
		"namespace": params.Namespace, "name": params.ApprovalPolicyName,
	}); err != nil {
		return err
	}

	result, err := a.client.GetApprovalPolicy(context.Background(), params.Namespace, params.ApprovalPolicyName)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal approval policy to YAML: %w", err)
	}
	fmt.Print(string(data))
	return nil
}

// DeletePolicy deletes an approval policy.
func (a *Approval) DeletePolicy(params PolicyDeleteParams) error {
	if err := cmdutil.RequireFields("delete", "approvalpolicy", map[string]string{
		"namespace": params.Namespace, "name": params.ApprovalPolicyName,
	}); err != nil {
		return err
	}

	if err := a.client.DeleteApprovalPolicy(context.Background(), params.Namespace, params.ApprovalPolicyName); err != nil {
		return err
	}

	fmt.Printf("ApprovalPolicy '%s' deleted\n", params.ApprovalPolicyName)
	return nil
}

func printRequestList(items []gen.ApprovalRequest) error {
	if len(items) == 0 {
		fmt.Println("No approval requests found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMPONENT\tENVIRONMENT\tREQUESTED\tREQUESTED BY\tPHASE\tAGE")

	for i := range items {
		ar := items[i]
		var component, environment, requested, requestedBy string
		if ar.Spec != nil {
			if ar.Spec.Target.Component != nil {
				component = *ar.Spec.Target.Component
			}
			if ar.Spec.Target.Environment != nil {
				environment = *ar.Spec.Target.Environment
			}
			if ar.Spec.Summary != nil && ar.Spec.Summary.Requested != nil {
				requested = *ar.Spec.Summary.Requested
			}
			requestedBy = ar.Spec.Requester.Id
			if ar.Spec.Requester.DisplayName != nil && *ar.Spec.Requester.DisplayName != "" {
				requestedBy = *ar.Spec.Requester.DisplayName
			}
		}

		phase := ""
		if ar.Status != nil && ar.Status.Phase != nil {
			phase = string(*ar.Status.Phase)
		}

		age := ""
		if ar.Metadata.CreationTimestamp != nil {
			age = utils.FormatAge(*ar.Metadata.CreationTimestamp)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			ar.Metadata.Name, dash(component), dash(environment), dash(requested),
			dash(requestedBy), dash(phase), age)
	}

	return w.Flush()
}

func printPolicyList(items []gen.ApprovalPolicy) error {
	if len(items) == 0 {
		fmt.Println("No approval policies found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tACTION\tENVIRONMENT\tAPPROVERS\tSUSPENDED\tAGE")

	for i := range items {
		ap := items[i]
		var action, environment, suspended string
		approvers := 0
		if ap.Spec != nil {
			action = ap.Spec.Action
			if ap.Spec.Scope != nil && ap.Spec.Scope.Environment != nil {
				environment = *ap.Spec.Scope.Environment
			}
			approvers = len(ap.Spec.Approvers)
			suspended = "false"
			if ap.Spec.Suspend != nil && *ap.Spec.Suspend {
				suspended = "true"
			}
		}

		age := ""
		if ap.Metadata.CreationTimestamp != nil {
			age = utils.FormatAge(*ap.Metadata.CreationTimestamp)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			ap.Metadata.Name, dash(action), dash(environment), approvers, dash(suspended), age)
	}

	return w.Flush()
}

// dash renders an empty column as "-" so a sparse row stays readable.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
