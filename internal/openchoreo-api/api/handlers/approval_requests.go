// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/api/gen"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	approvalrequestsvc "github.com/openchoreo/openchoreo/internal/openchoreo-api/services/approvalrequest"
	"github.com/openchoreo/openchoreo/internal/server/middleware/audit"
)

// ListApprovalRequests returns a paginated list of approval requests within a namespace.
func (h *Handler) ListApprovalRequests(
	ctx context.Context,
	request gen.ListApprovalRequestsRequestObject,
) (gen.ListApprovalRequestsResponseObject, error) {
	h.logger.Debug("ListApprovalRequests called", "namespaceName", request.NamespaceName)

	opts := NormalizeListOptions(request.Params.Limit, request.Params.Cursor, nil)

	filter := approvalrequestsvc.ListFilter{}
	if request.Params.Phase != nil {
		filter.Phase = string(*request.Params.Phase)
	}
	if request.Params.Environment != nil {
		filter.Environment = *request.Params.Environment
	}

	result, err := h.services.ApprovalRequestService.ListApprovalRequests(ctx, request.NamespaceName, filter, opts)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.ListApprovalRequests403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			return gen.ListApprovalRequests400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to list approval requests", "error", err)
		return gen.ListApprovalRequests500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	items, err := convertList[openchoreov1alpha1.ApprovalRequest, gen.ApprovalRequest](result.Items)
	if err != nil {
		h.logger.Error("Failed to convert approval requests", "error", err)
		return gen.ListApprovalRequests500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.ListApprovalRequests200JSONResponse{
		Items:      items,
		Pagination: ToPagination(result),
	}, nil
}

// GetApprovalRequest returns details of a specific approval request.
func (h *Handler) GetApprovalRequest(
	ctx context.Context,
	request gen.GetApprovalRequestRequestObject,
) (gen.GetApprovalRequestResponseObject, error) {
	h.logger.Debug("GetApprovalRequest called", "namespaceName", request.NamespaceName, "approvalRequestName", request.ApprovalRequestName)

	ar, err := h.services.ApprovalRequestService.GetApprovalRequest(ctx, request.NamespaceName, request.ApprovalRequestName)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.GetApprovalRequest403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, approvalrequestsvc.ErrApprovalRequestNotFound) {
			return gen.GetApprovalRequest404JSONResponse{NotFoundJSONResponse: notFound("ApprovalRequest")}, nil
		}
		h.logger.Error("Failed to get approval request", "error", err)
		return gen.GetApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	genAR, err := convert[openchoreov1alpha1.ApprovalRequest, gen.ApprovalRequest](*ar)
	if err != nil {
		h.logger.Error("Failed to convert approval request", "error", err)
		return gen.GetApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.GetApprovalRequest200JSONResponse(genAR), nil
}

// DecideApprovalRequest records an approve or reject on a pending request.
func (h *Handler) DecideApprovalRequest(
	ctx context.Context,
	request gen.DecideApprovalRequestRequestObject,
) (gen.DecideApprovalRequestResponseObject, error) {
	h.logger.Info("DecideApprovalRequest called", "namespaceName", request.NamespaceName, "approvalRequestName", request.ApprovalRequestName)

	if request.Body == nil {
		return gen.DecideApprovalRequest400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	result := openchoreov1alpha1.ApprovalResult(request.Body.Result)
	comment := ""
	if request.Body.Comment != nil {
		comment = *request.Body.Comment
	}

	ar, err := h.services.ApprovalRequestService.DecideApprovalRequest(
		ctx, request.NamespaceName, request.ApprovalRequestName, result, comment)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrForbidden):
			return gen.DecideApprovalRequest403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		case errors.Is(err, approvalrequestsvc.ErrApprovalRequestNotFound):
			return gen.DecideApprovalRequest404JSONResponse{NotFoundJSONResponse: notFound("ApprovalRequest")}, nil
		case errors.Is(err, approvalrequestsvc.ErrPolicyNotFound):
			return gen.DecideApprovalRequest404JSONResponse{NotFoundJSONResponse: notFound("ApprovalPolicy")}, nil
		// Not an approver, and self-approval, are refusals of this specific
		// decision rather than of the caller's access to approvals generally, so
		// they carry their own reason instead of a bare forbidden().
		case errors.Is(err, approvalrequestsvc.ErrNotAnApprover),
			errors.Is(err, approvalrequestsvc.ErrSelfApproval):
			return gen.DecideApprovalRequest403JSONResponse{
				ForbiddenJSONResponse: gen.ForbiddenJSONResponse{Error: err.Error(), Code: gen.FORBIDDEN},
			}, nil
		case errors.Is(err, approvalrequestsvc.ErrNotPending):
			return gen.DecideApprovalRequest409JSONResponse{ConflictJSONResponse: conflict(err.Error())}, nil
		case errors.Is(err, approvalrequestsvc.ErrCommentRequired):
			return gen.DecideApprovalRequest422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(err.Error())}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			return gen.DecideApprovalRequest400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to record approval decision", "error", err)
		return gen.DecideApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, ID: string(ar.UID), Name: ar.Name})

	genAR, err := convert[openchoreov1alpha1.ApprovalRequest, gen.ApprovalRequest](*ar)
	if err != nil {
		h.logger.Error("Failed to convert approval request", "error", err)
		return gen.DecideApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	h.logger.Info("Approval decision recorded",
		"namespaceName", request.NamespaceName, "approvalRequest", ar.Name, "result", result)
	return gen.DecideApprovalRequest200JSONResponse(genAR), nil
}

// CancelApprovalRequest withdraws a pending request.
func (h *Handler) CancelApprovalRequest(
	ctx context.Context,
	request gen.CancelApprovalRequestRequestObject,
) (gen.CancelApprovalRequestResponseObject, error) {
	h.logger.Info("CancelApprovalRequest called", "namespaceName", request.NamespaceName, "approvalRequestName", request.ApprovalRequestName)

	ar, err := h.services.ApprovalRequestService.CancelApprovalRequest(ctx, request.NamespaceName, request.ApprovalRequestName)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrForbidden):
			return gen.CancelApprovalRequest403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		case errors.Is(err, approvalrequestsvc.ErrApprovalRequestNotFound):
			return gen.CancelApprovalRequest404JSONResponse{NotFoundJSONResponse: notFound("ApprovalRequest")}, nil
		case errors.Is(err, approvalrequestsvc.ErrNotRequester):
			return gen.CancelApprovalRequest403JSONResponse{
				ForbiddenJSONResponse: gen.ForbiddenJSONResponse{Error: err.Error(), Code: gen.FORBIDDEN},
			}, nil
		case errors.Is(err, approvalrequestsvc.ErrNotPending):
			return gen.CancelApprovalRequest409JSONResponse{ConflictJSONResponse: conflict(err.Error())}, nil
		}
		h.logger.Error("Failed to cancel approval request", "error", err)
		return gen.CancelApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, ID: string(ar.UID), Name: ar.Name})

	genAR, err := convert[openchoreov1alpha1.ApprovalRequest, gen.ApprovalRequest](*ar)
	if err != nil {
		h.logger.Error("Failed to convert approval request", "error", err)
		return gen.CancelApprovalRequest500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.CancelApprovalRequest200JSONResponse(genAR), nil
}
