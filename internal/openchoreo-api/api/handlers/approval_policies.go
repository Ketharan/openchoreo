// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"net/http"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/api/gen"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	approvalpolicysvc "github.com/openchoreo/openchoreo/internal/openchoreo-api/services/approvalpolicy"
	"github.com/openchoreo/openchoreo/internal/server/middleware/audit"
)

// ListApprovalPolicies returns a paginated list of approval policies within a namespace.
func (h *Handler) ListApprovalPolicies(
	ctx context.Context,
	request gen.ListApprovalPoliciesRequestObject,
) (gen.ListApprovalPoliciesResponseObject, error) {
	h.logger.Debug("ListApprovalPolicies called", "namespaceName", request.NamespaceName)

	opts := NormalizeListOptions(request.Params.Limit, request.Params.Cursor, nil)

	result, err := h.services.ApprovalPolicyService.ListApprovalPolicies(ctx, request.NamespaceName, opts)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.ListApprovalPolicies403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			return gen.ListApprovalPolicies400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to list approval policies", "error", err)
		return gen.ListApprovalPolicies500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	items, err := convertList[openchoreov1alpha1.ApprovalPolicy, gen.ApprovalPolicy](result.Items)
	if err != nil {
		h.logger.Error("Failed to convert approval policies", "error", err)
		return gen.ListApprovalPolicies500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.ListApprovalPolicies200JSONResponse{
		Items:      items,
		Pagination: ToPagination(result),
	}, nil
}

// CreateApprovalPolicy creates a new approval policy within a namespace.
func (h *Handler) CreateApprovalPolicy(
	ctx context.Context,
	request gen.CreateApprovalPolicyRequestObject,
) (gen.CreateApprovalPolicyResponseObject, error) {
	h.logger.Info("CreateApprovalPolicy called", "namespaceName", request.NamespaceName)

	if request.Body == nil {
		return gen.CreateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	apCR, err := convert[gen.ApprovalPolicy, openchoreov1alpha1.ApprovalPolicy](*request.Body)
	if err != nil {
		h.logger.Error("Failed to convert create request", "error", err)
		return gen.CreateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest("Invalid request body")}, nil
	}

	created, err := h.services.ApprovalPolicyService.CreateApprovalPolicy(ctx, request.NamespaceName, &apCR)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.CreateApprovalPolicy403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, approvalpolicysvc.ErrApprovalPolicyAlreadyExists) {
			return gen.CreateApprovalPolicy409JSONResponse{ConflictJSONResponse: conflict("Approval policy already exists")}, nil
		}
		// A policy naming an unenforceable action is the user's mistake, and the
		// message lists what they could have named instead.
		if errors.Is(err, approvalpolicysvc.ErrActionNotApprovable) {
			return gen.CreateApprovalPolicy422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(err.Error())}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			if validationErr.StatusCode == http.StatusUnprocessableEntity {
				return gen.CreateApprovalPolicy422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(validationErr.Msg)}, nil
			}
			return gen.CreateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to create approval policy", "error", err)
		return gen.CreateApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, ID: string(created.UID), Name: created.Name})

	genAP, err := convert[openchoreov1alpha1.ApprovalPolicy, gen.ApprovalPolicy](*created)
	if err != nil {
		h.logger.Error("Failed to convert created approval policy", "error", err)
		return gen.CreateApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	h.logger.Info("Approval policy created", "namespaceName", request.NamespaceName, "approvalPolicy", created.Name)
	return gen.CreateApprovalPolicy201JSONResponse(genAP), nil
}

// GetApprovalPolicy returns details of a specific approval policy.
func (h *Handler) GetApprovalPolicy(
	ctx context.Context,
	request gen.GetApprovalPolicyRequestObject,
) (gen.GetApprovalPolicyResponseObject, error) {
	h.logger.Debug("GetApprovalPolicy called", "namespaceName", request.NamespaceName, "approvalPolicyName", request.ApprovalPolicyName)

	ap, err := h.services.ApprovalPolicyService.GetApprovalPolicy(ctx, request.NamespaceName, request.ApprovalPolicyName)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.GetApprovalPolicy403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, approvalpolicysvc.ErrApprovalPolicyNotFound) {
			return gen.GetApprovalPolicy404JSONResponse{NotFoundJSONResponse: notFound("ApprovalPolicy")}, nil
		}
		h.logger.Error("Failed to get approval policy", "error", err)
		return gen.GetApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	genAP, err := convert[openchoreov1alpha1.ApprovalPolicy, gen.ApprovalPolicy](*ap)
	if err != nil {
		h.logger.Error("Failed to convert approval policy", "error", err)
		return gen.GetApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.GetApprovalPolicy200JSONResponse(genAP), nil
}

// UpdateApprovalPolicy replaces an existing approval policy (full update).
func (h *Handler) UpdateApprovalPolicy(
	ctx context.Context,
	request gen.UpdateApprovalPolicyRequestObject,
) (gen.UpdateApprovalPolicyResponseObject, error) {
	h.logger.Info("UpdateApprovalPolicy called", "namespaceName", request.NamespaceName, "approvalPolicyName", request.ApprovalPolicyName)

	if request.Body == nil {
		return gen.UpdateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	apCR, err := convert[gen.ApprovalPolicy, openchoreov1alpha1.ApprovalPolicy](*request.Body)
	if err != nil {
		h.logger.Error("Failed to convert update request", "error", err)
		return gen.UpdateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest("Invalid request body")}, nil
	}
	apCR.Name = request.ApprovalPolicyName

	updated, err := h.services.ApprovalPolicyService.UpdateApprovalPolicy(ctx, request.NamespaceName, &apCR)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.UpdateApprovalPolicy403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, approvalpolicysvc.ErrApprovalPolicyNotFound) {
			return gen.UpdateApprovalPolicy404JSONResponse{NotFoundJSONResponse: notFound("ApprovalPolicy")}, nil
		}
		if errors.Is(err, approvalpolicysvc.ErrActionNotApprovable) {
			return gen.UpdateApprovalPolicy422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(err.Error())}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			if validationErr.StatusCode == http.StatusUnprocessableEntity {
				return gen.UpdateApprovalPolicy422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(validationErr.Msg)}, nil
			}
			return gen.UpdateApprovalPolicy400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to update approval policy", "error", err)
		return gen.UpdateApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, ID: string(updated.UID), Name: updated.Name})

	genAP, err := convert[openchoreov1alpha1.ApprovalPolicy, gen.ApprovalPolicy](*updated)
	if err != nil {
		h.logger.Error("Failed to convert updated approval policy", "error", err)
		return gen.UpdateApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.UpdateApprovalPolicy200JSONResponse(genAP), nil
}

// DeleteApprovalPolicy deletes an approval policy by name.
func (h *Handler) DeleteApprovalPolicy(
	ctx context.Context,
	request gen.DeleteApprovalPolicyRequestObject,
) (gen.DeleteApprovalPolicyResponseObject, error) {
	h.logger.Info("DeleteApprovalPolicy called", "namespaceName", request.NamespaceName, "approvalPolicyName", request.ApprovalPolicyName)

	err := h.services.ApprovalPolicyService.DeleteApprovalPolicy(ctx, request.NamespaceName, request.ApprovalPolicyName)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.DeleteApprovalPolicy403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, approvalpolicysvc.ErrApprovalPolicyNotFound) {
			return gen.DeleteApprovalPolicy404JSONResponse{NotFoundJSONResponse: notFound("ApprovalPolicy")}, nil
		}
		h.logger.Error("Failed to delete approval policy", "error", err)
		return gen.DeleteApprovalPolicy500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.DeleteApprovalPolicy204Response{}, nil
}
