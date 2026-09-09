// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package releasebinding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/approval"
	authz "github.com/openchoreo/openchoreo/internal/authz/core"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	"github.com/openchoreo/openchoreo/internal/server/middleware/auth"
)

const (
	resourceTypeReleaseBinding = "releasebinding"
)

// releaseBindingServiceWithAuthz wraps a Service and adds authorization checks.
// Handlers should use this. Other services should use the unwrapped Service directly.
type releaseBindingServiceWithAuthz struct {
	internal  Service
	k8sClient client.Client
	authz     *services.AuthzChecker
}

var _ Service = (*releaseBindingServiceWithAuthz)(nil)

// NewServiceWithAuthz creates a release binding service with authorization checks.
func NewServiceWithAuthz(k8sClient client.Client, authzPDP authz.PDP, logger *slog.Logger) Service {
	return &releaseBindingServiceWithAuthz{
		internal:  NewService(k8sClient, logger),
		k8sClient: k8sClient,
		authz:     services.NewAuthzChecker(authzPDP, logger),
	}
}

func (s *releaseBindingServiceWithAuthz) CreateReleaseBinding(ctx context.Context, namespaceName string, rb *openchoreov1alpha1.ReleaseBinding) (*openchoreov1alpha1.ReleaseBinding, error) {
	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionCreateReleaseBinding,
		ResourceType: resourceTypeReleaseBinding,
		ResourceID:   rb.Name,
		Hierarchy: authz.ResourceHierarchy{
			Namespace: namespaceName,
			Project:   rb.Spec.Owner.ProjectName,
			Component: rb.Spec.Owner.ComponentName,
		},
		Context: authz.Context{
			// TODO: pass kind discriminator once ReleaseBindingSpec.Environment gains a kind field
			Resource: authz.ResourceAttribute{
				Environment: services.FormatDualScopedResourceName(namespaceName, rb.Spec.Environment, false)},
		},
	}); err != nil {
		return nil, err
	}
	return s.internal.CreateReleaseBinding(ctx, namespaceName, rb)
}

func (s *releaseBindingServiceWithAuthz) UpdateReleaseBinding(ctx context.Context, namespaceName string, rb *openchoreov1alpha1.ReleaseBinding) (*openchoreov1alpha1.ReleaseBinding, error) {
	// Fetch the existing release binding to get owner info for authz
	existing, err := s.internal.GetReleaseBinding(ctx, namespaceName, rb.Name)
	if err != nil {
		return nil, err
	}

	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionUpdateReleaseBinding,
		ResourceType: resourceTypeReleaseBinding,
		ResourceID:   rb.Name,
		Hierarchy: authz.ResourceHierarchy{
			Namespace: namespaceName,
			Project:   existing.Spec.Owner.ProjectName,
			Component: existing.Spec.Owner.ComponentName,
		},
		Context: authz.Context{
			// TODO: pass kind discriminator once ReleaseBindingSpec.Environment gains a kind field
			Resource: authz.ResourceAttribute{Environment: services.FormatDualScopedResourceName(namespaceName, existing.Spec.Environment, false)},
		},
	}); err != nil {
		return nil, err
	}

	// The approval gate runs after authorization, and only for a subject already
	// permitted to make this change: approval is a second check on people who
	// hold the permission, not a way to grant it to people who do not.
	//
	// It lives here rather than inside AuthzChecker.Check because the gate needs
	// the desired state to fingerprint, and CheckRequest deliberately carries no
	// payload. This wrapper already holds both the existing and incoming specs.
	if err := s.enforceApproval(ctx, namespaceName, existing, rb); err != nil {
		return nil, err
	}

	return s.internal.UpdateReleaseBinding(ctx, namespaceName, rb)
}

// enforceApproval blocks an update that a policy gates and nobody has approved.
func (s *releaseBindingServiceWithAuthz) enforceApproval(
	ctx context.Context,
	namespaceName string,
	existing, incoming *openchoreov1alpha1.ReleaseBinding,
) error {
	requestedState, err := json.Marshal(incoming.Spec)
	if err != nil {
		return fmt.Errorf("serialising requested release binding state: %w", err)
	}

	changeKind := approval.ClassifyReleaseBindingChange(&existing.Spec, &incoming.Spec)
	attempt := approval.Attempt{
		Action: authz.ActionUpdateReleaseBinding,
		Target: openchoreov1alpha1.ApprovalTarget{
			Kind:        "ReleaseBinding",
			Name:        incoming.Name,
			Project:     existing.Spec.Owner.ProjectName,
			Component:   existing.Spec.Owner.ComponentName,
			Environment: existing.Spec.Environment,
		},
		ChangeKind: changeKind,
		Subject:    subjectFromContext(ctx),
	}

	verdict, err := approval.Evaluate(ctx, s.k8sClient, namespaceName, attempt, requestedState)
	if err != nil {
		// Fail closed: if the gate cannot be evaluated we do not know whether this
		// change needed approval, and assuming it did not disables the control.
		return fmt.Errorf("evaluating approval gate: %w", err)
	}
	if verdict.Allowed() {
		return nil
	}

	// A blocked change with no request yet opens one, rather than telling the
	// developer they are blocked and leaving them to work out how to ask.
	if verdict.Outcome == approval.OutcomeRequired {
		opened, openErr := approval.Open(ctx, s.k8sClient, approval.OpenParams{
			Namespace:      namespaceName,
			Policy:         verdict.Policy,
			Attempt:        attempt,
			RequestedState: requestedState,
			Fingerprint:    verdict.Fingerprint,
			Summary:        summariseReleaseBindingChange(changeKind, existing, incoming),
			Message:        incoming.Annotations[approval.MessageAnnotation],
		})
		if openErr != nil {
			return openErr
		}
		verdict.Request = opened
	}

	return approval.AsError(verdict)
}

// summariseReleaseBindingChange renders what an approver needs in order to
// decide: what runs now, what would run, and what differs. It is held on the
// request so the decision does not depend on the approver holding read access to
// the requester's project.
func summariseReleaseBindingChange(
	changeKind openchoreov1alpha1.ChangeKind,
	existing, incoming *openchoreov1alpha1.ReleaseBinding,
) *openchoreov1alpha1.ApprovalSummary {
	summary := &openchoreov1alpha1.ApprovalSummary{
		ChangeKind: changeKind,
		Current:    existing.Spec.ReleaseName,
		Requested:  incoming.Spec.ReleaseName,
	}

	if existing.Spec.ReleaseName != incoming.Spec.ReleaseName {
		summary.Details = append(summary.Details,
			fmt.Sprintf("release: %s → %s", existing.Spec.ReleaseName, incoming.Spec.ReleaseName))
	}
	if existing.Spec.State != incoming.Spec.State {
		summary.Details = append(summary.Details,
			fmt.Sprintf("state: %s → %s", existing.Spec.State, incoming.Spec.State))
	}
	// Overrides are compared as a whole rather than field by field: their shape is
	// component-type specific, so the honest summary is that they changed, with
	// the request's requestedState carrying the detail.
	if !equalRaw(existing.Spec.ComponentTypeEnvironmentConfigs, incoming.Spec.ComponentTypeEnvironmentConfigs) {
		summary.Details = append(summary.Details, "componentTypeEnvironmentConfigs changed")
	}
	if !equalRaw(existing.Spec.WorkloadOverrides, incoming.Spec.WorkloadOverrides) {
		summary.Details = append(summary.Details, "workloadOverrides changed")
	}
	if len(summary.Details) == 0 {
		summary.Details = append(summary.Details, "no field-level differences detected")
	}
	return summary
}

func equalRaw[T any](a, b *T) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}

// subjectFromContext resolves who is acting, for attribution on the request.
func subjectFromContext(ctx context.Context) openchoreov1alpha1.ApprovalSubject {
	sc, ok := auth.GetSubjectContextFromContext(ctx)
	if !ok || sc == nil {
		return openchoreov1alpha1.ApprovalSubject{}
	}
	return openchoreov1alpha1.ApprovalSubject{ID: sc.ID, DisplayName: sc.ID}
}

func (s *releaseBindingServiceWithAuthz) ListReleaseBindings(ctx context.Context, namespaceName, componentName string, opts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ReleaseBinding], error) {
	return services.FilteredList(ctx, opts, s.authz,
		func(ctx context.Context, pageOpts services.ListOptions) (*services.ListResult[openchoreov1alpha1.ReleaseBinding], error) {
			return s.internal.ListReleaseBindings(ctx, namespaceName, componentName, pageOpts)
		},
		func(rb openchoreov1alpha1.ReleaseBinding) services.CheckRequest {
			return services.CheckRequest{
				Action:       authz.ActionViewReleaseBinding,
				ResourceType: resourceTypeReleaseBinding,
				ResourceID:   rb.Name,
				Hierarchy: authz.ResourceHierarchy{
					Namespace: namespaceName,
					Project:   rb.Spec.Owner.ProjectName,
					Component: rb.Spec.Owner.ComponentName,
				},
				Context: authz.Context{
					// TODO: pass kind discriminator once ReleaseBindingSpec.Environment gains a kind field
					Resource: authz.ResourceAttribute{
						Environment: services.FormatDualScopedResourceName(namespaceName, rb.Spec.Environment, false)},
				},
			}
		},
	)
}

func (s *releaseBindingServiceWithAuthz) GetReleaseBinding(ctx context.Context, namespaceName, releaseBindingName string) (*openchoreov1alpha1.ReleaseBinding, error) {
	// Fetch the release binding first to get owner info for authz
	rb, err := s.internal.GetReleaseBinding(ctx, namespaceName, releaseBindingName)
	if err != nil {
		return nil, err
	}

	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionViewReleaseBinding,
		ResourceType: resourceTypeReleaseBinding,
		ResourceID:   releaseBindingName,
		Hierarchy: authz.ResourceHierarchy{
			Namespace: namespaceName,
			Project:   rb.Spec.Owner.ProjectName,
			Component: rb.Spec.Owner.ComponentName,
		},
		Context: authz.Context{
			// TODO: pass kind discriminator once ReleaseBindingSpec.Environment gains a kind field
			Resource: authz.ResourceAttribute{
				Environment: services.FormatDualScopedResourceName(namespaceName, rb.Spec.Environment, false)},
		},
	}); err != nil {
		return nil, err
	}
	return rb, nil
}

func (s *releaseBindingServiceWithAuthz) DeleteReleaseBinding(ctx context.Context, namespaceName, releaseBindingName string) error {
	// Fetch the release binding first to get owner info for authz
	rb, err := s.internal.GetReleaseBinding(ctx, namespaceName, releaseBindingName)
	if err != nil {
		return err
	}

	if err := s.authz.Check(ctx, services.CheckRequest{
		Action:       authz.ActionDeleteReleaseBinding,
		ResourceType: resourceTypeReleaseBinding,
		ResourceID:   releaseBindingName,
		Hierarchy: authz.ResourceHierarchy{
			Namespace: namespaceName,
			Project:   rb.Spec.Owner.ProjectName,
			Component: rb.Spec.Owner.ComponentName,
		},
		Context: authz.Context{
			// TODO: pass kind discriminator once ReleaseBindingSpec.Environment gains a kind field
			Resource: authz.ResourceAttribute{
				Environment: services.FormatDualScopedResourceName(namespaceName, rb.Spec.Environment, false)},
		},
	}); err != nil {
		return err
	}
	return s.internal.DeleteReleaseBinding(ctx, namespaceName, releaseBindingName)
}
