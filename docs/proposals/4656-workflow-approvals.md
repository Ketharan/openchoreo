# Workflow Approvals

**Authors**:
_@Ketharan_

**Reviewers**:
_TBD_

**Created Date**:
_2026-09-09_

**Status**:
_Draft — not yet submitted_

**Related Issues/PRs**:
_[#4656](https://github.com/openchoreo/openchoreo/issues/4656) (epic), [#3561](https://github.com/openchoreo/openchoreo/issues/3561) (related — external approval webhooks), [#2650](https://github.com/openchoreo/openchoreo/issues/2650) / [#2651](https://github.com/openchoreo/openchoreo/pull/2651) (removal of the previous approval fields)_

---

## Summary

OpenChoreo has no way to require human sign-off before a sensitive action takes effect. Any subject
holding the relevant permission acts alone and immediately.

This proposal introduces **Workflow Approvals**: a platform engineer declares that a given action,
in a given scope, requires approval; attempting it opens an **approval request** instead of taking
effect; designated approvers are notified and approve or reject with a comment; only then does the
action land.

Two new namespaced CRDs — `ApprovalPolicy` and `ApprovalRequest` — plus an enforcement point in the
API layer. The set of gated actions is open; **promotion of a component to a protected environment
is the first and only one in this proposal**.

---

## Motivation

Promotion today is a single unreviewed write: advancing the release pin on a binding
(`ReleaseBinding.spec.releaseName` and its `ProjectReleaseBinding` / `ResourceReleaseBinding`
equivalents), via the REST API, `occ`, GitOps, or `kubectl edit`. Whoever holds
`releasebinding:update` on the target environment can ship to production unilaterally.

Organisations adopting OpenChoreo as an internal developer platform generally cannot accept that for
production, and today they work around it in one of two ways:

- **Restrict rather than approve.** The existing ABAC machinery already supports this — an
  `AuthzCondition` with `resource.environment in ["dev", "staging"]` prevents developers promoting
  to production. This delivers the security property but no workflow: no request, no notification,
  no record of who agreed to what. The holder of the permission still acts alone.
- **Approve in Git.** Put the binding manifests in the GitOps repo and gate it with `CODEOWNERS` and
  required reviews. A real approval with an audit trail, but it lives in GitHub — invisible to the
  portal, `occ` and MCP, bypassed by anyone with API access, and unable to express "this environment
  needs approval" as platform policy. This is what [#3561](https://github.com/openchoreo/openchoreo/issues/3561)
  refers to as "approval gates only when running with a customized GitOps workflow".

Neither is the feature. The prior art is
[Workflow Approvals in Choreo](https://wso2.com/library/blogs/workflow-approvals-choreo/), which
this proposal adapts to OpenChoreo's CRD, authz and GitOps architecture rather than porting.

### Why this is not a revert of #2651

`requiresApproval` and `isManualApprovalRequired` previously existed on
`DeploymentPipeline.spec.promotionPaths[].targetEnvironmentRefs[]` and were removed in #2651. The
stated reason in #2650 was that they were *"purely declarative metadata with no controller logic
enforcing them"* — *"no controller, webhook, or business logic reads or acts on them."*

That diagnosis is this proposal's requirement. The fields were removed because nothing enforced
them, so the work is the enforcement and the lifecycle, not the flags. Approval is also not a
property of a deployment pipeline — it is a property of an *action* — which is why the model below
is action-centric and lives outside `DeploymentPipeline`. Reintroducing a boolean on
`DeploymentPipeline` is explicitly out of scope.

---

## Goals

- A platform engineer can declare that an action in a scope requires approval, without code changes.
- Attempting a gated action produces a **pending request**, not a silent failure and not an applied
  change.
- Designated approvers are notified, can see what is being requested, and approve or reject with a
  comment.
- An approved request permits exactly what was reviewed — once, and not a substituted change.
- Every request, decision and cancellation is durably auditable.
- Approver identity reuses the existing Casbin/CEL authz model rather than a parallel one.
- Adding a second gated action requires registration plus one enforcement call — no CRD, approver
  model or portal change.
- Actions with no policy behave exactly as they do today: no new latency, no new failure mode.

---

## Non-Goals

- **Gated actions beyond promotion.** The model must not foreclose them (`component:exec`, secret
  access, API subscription, destructive deletes are all plausible); this proposal ships one.
- **Multi-step approval chains, quorum ("2 of 3"), or delegation.**
- **Time-based auto-expiry of pending requests**, and scheduled or windowed execution.
- **Notification channels beyond email and webhook.**
- **External/automated decision sources.** That is #3561, which should plug into this framework as
  another decision source rather than growing a second, incompatible gate.
- **Closing the `kubectl` / GitOps bypass.** See *Impact* — this is a known, documented limitation of
  the first increment, addressed by a follow-up.

---

## Impact

| Area | Impact |
|---|---|
| **API (`openchoreo-api`)** | 9 new endpoints; enforcement in the `releasebinding` authz wrapper; 2 new services. |
| **CRDs** | 2 new namespaced kinds. No changes to existing kinds. |
| **Authz** | 8 new actions, `resource.environment` conditions registered for approval actions, and a new `Approvable` flag on the action registry. |
| **CLI (`occ`)** | New `occ approval` command tree (`list`, `get`, `approve`, `reject`, `cancel`, `policy …`). |
| **Audit** | 5 new audited operations, generated from the spec. Categorised as `CategoryAuthorization`. |
| **Controllers** | None in this increment; an `ApprovalRequest` controller is required as a follow-up (see *Deferred*). |
| **Helm** | New CRDs in the control-plane chart; `openchoreo-api` ClusterRole extended. |
| **Portal** | Not in this proposal. Tracked in `openchoreo/backstage-plugins`. |

**Backward compatibility.** Additive. No existing CRD, endpoint or action changes shape. An
installation that creates no `ApprovalPolicy` behaves exactly as before — the gate short-circuits
when no policy matches, and adds one list call to the update path when one does.

**Operational.** Requests are Kubernetes objects and accumulate until the follow-up controller
implements TTL cleanup.

---

## Design

### Core model

**`ApprovalPolicy`** — platform-engineer intent: which action, in which scope, requires approval;
who may approve; how long decided requests are kept.

```yaml
apiVersion: openchoreo.dev/v1alpha1
kind: ApprovalPolicy
metadata:
  name: production-changes
  namespace: acme
spec:
  action: releasebinding:update
  scope:
    environment: production
    fromEnvironment: dev        # optional: gate dev→prod, allow staging→prod
  changes: [ReleaseChange, ConfigChange, Undeploy]
  approvers:
    - roleRef: { kind: ClusterAuthzRole, name: release-manager }
      scope: { project: checkout }
    - entitlement: { claim: email, value: someone@acme.com }
  allowSelfApproval: false
  suspend: false
  requestTTLAfterCompletion: 90d
```

**`ApprovalRequest`** — one pending decision. Modelled on `WorkflowRun`: a run-instance kind with a
terminal state and a TTL, because that is the same lifecycle. Every field except `decision` is
immutable — an approver must not review one change while a different one executes.

Phases: `Pending → Approved → Executed`, or `Pending → Rejected | Cancelled`. `Approved` and
`Executed` are distinct because approving and performing are distinct events (see *Open question 2*).

### Two classes of gated action

This is the part that has to be right for the model to generalise. OpenChoreo actions fall into two
classes and they cannot share one enforcement point:

- **Synchronous actions** — invoked once through the API and done (`component:exec`, secret access,
  a subscription). The chokepoint is the API handler.
- **Reconciled actions** — expressed as desired state and continuously actuated by a controller
  (promotion, and most of the CRD surface). For these, the gate must withhold *actuation*, not
  reject the *write*.

Rejecting a gated write in a validating admission webhook is the obvious move and is wrong here, on
three independent grounds:

1. It contradicts the repo's stated convention that cross-resource validation belongs in the
   controller (`internal/webhook/releasebinding/webhook.go` ships a deliberately empty validator
   saying exactly this).
2. It breaks the Flux GitOps path: a rejected write is not a clean "pending" state but a permanently
   failing `Kustomization`, and anything gated on that Kustomization's readiness stalls. Approval
   pending is a normal business state; it must not page someone.
3. `webhook.CustomValidator` receives only old and new objects — no `admission.Request`, therefore
   no requester identity. Both webhooks also run `failurePolicy=fail`.

Accepting the write and withholding actuation keeps GitOps healthy, records intent in `spec`, and
records why nothing happened in `status`. The honest cost is that `spec` may describe something not
yet live, so `status` becomes the authority on what is actually running.

**In this increment the gate is at the API layer only** — in the `releasebinding` authz wrapper,
after the permission check. That covers the REST API, `occ`, the portal and MCP. It does **not**
cover `kubectl` or a GitOps controller writing the CR directly. See *Deferred*.

Note that the gate cannot live inside `AuthzChecker.Check`, despite that being the single shared
authorization path: `CheckRequest` deliberately carries no payload, and the gate needs the desired
state to fingerprint. It goes in the per-service wrapper, which already holds both specs.

### Binding an approval to one specific change

`spec.requestedState` holds the **complete** intent — for a promotion, the whole desired binding
spec, not just the release name. Binding an approval to the release name alone would let environment
configs, trait configs or workload overrides change between review and execution, shipping
unreviewed configuration under a reviewed version number.

`spec.stateFingerprint` is a SHA-256 of the canonicalised requested state. The action proceeds only
when the state being applied hashes to the value on an approved request. This is what prevents an
approval being replayed for a different change, and it makes approvals single-use in practice.

### Who may approve

Approvers are expressed with one mechanism covering roles, individuals and IdP groups, because authz
subjects are already `{claim, value}` entitlements. This mirrors `AuthzRoleBinding` rather than
introducing a parallel permission system.

Two consequences follow from OpenChoreo having **no user directory**:

- The platform can answer *"may **you** approve this?"* but never *"who may approve this?"* — a
  claim match cannot be enumerated. A pending request can therefore name the role or the named
  individuals a policy declares, never a resolved roster. Named individuals are the only enumerable
  form, which is a reason to support them and not only roles.
- Notification cannot resolve "the appointed approvers" into addresses. See *Open question 3*.

An approver may hold no `component:view` on the requesting project. Rather than requiring that, or
punching a read hole, `spec.summary` carries a self-contained snapshot — current release, requested
release, and the differences — rendered at request time. Deciding then needs only
`approvalrequest:view`, and the record preserves what the approver was actually shown, which a later
lookup cannot reconstruct.

`approvalrequest:decide` is a separate action from `:update` — deciding is not editing. Holding it
is necessary but not sufficient: the policy's `approvers` list is the real check, enforced in the
service.

### Authorization actions

`approvalpolicy:{create,view,update,delete}` and
`approvalrequest:{create,view,cancel,decide}`, each with `resource.environment` registered so "may
approve promotions into production" is expressible with the CEL conditions that already exist.

`spec.action` on a policy is validated against a registry of approval-capable actions — a new
`Approvable` flag on the existing `Action` metadata in `internal/authz/core/actions.go`. Gating an
action with no enforcement point would produce a policy that appears to work and never fires, which
is precisely the failure #2650 removed. Only `releasebinding:update` is approvable today.

### Audit

All five state-modifying operations are audited, generated from the OpenAPI spec by `tools/auditgen`
and categorised as `CategoryAuthorization` — an approval policy decides who may release a change and
a decision records someone exercising that authority, so auditors want them alongside role changes.

---

## Open questions

These are the ones that should be settled in review; the reference implementation picked defensible
answers but they are not agreed. A full list is maintained alongside the epic.

1. **Does approval gate people who already hold the permission, or grant it to people who do not?**
   The implementation assumes the former — approval is a *second* check, and the requester's own
   authorization is still required. This shapes who may request, and what break-glass means.
2. **Does approval execute the action, or unlock a retry?** The implementation unlocks a retry
   (as Choreo does). Executing on approval is better UX — nobody expects to approve a deploy and have
   nothing deploy — but it requires the platform to act on the requester's behalf after their session
   ends, and re-checking their permission then would require storing their entitlement claims on the
   request, with the staleness and disclosure that implies. Retry is the only mode where the action
   runs under a live, current authorization for the person it is attributed to. Recommended as a
   later `spec.onApproval: Retry|Execute` field, defaulting to `Retry`.
3. **Notification recipients**, given no user directory. A webhook channel routing into where
   approvers already are is the honest primitive; a policy with no reachable target should be flagged
   at configuration time.
4. **Is the gate about release changes, or any change to a protected environment?** `spec.changes`
   currently defaults to all kinds, so rollback (a `ReleaseChange` moving backwards), undeploy and
   config-only edits are gated unless a policy narrows itself. Gating rollback slows incident
   recovery; not gating it leaves an unreviewed path into production.
5. **Break-glass**, and behaviour when authorization is disabled entirely
   (`internal/authz/disabled_authorizer.go`). Without an audited override, incident response will
   route around the feature. Silently degrading to no-gate is the worst outcome.
6. **Self-protection.** Anyone holding `approvalpolicy:update` can set `suspend: true` and promote
   freely. A governance control with an unguarded off switch needs a non-deadlocking answer.

---

## Deferred to follow-up work

Deliberately not in this proposal, and required before the feature is complete:

- **`ApprovalRequest` lifecycle controller** — TTL-based cleanup, staleness detection (requested
  release deleted or superseded), and phase reconciliation. Without it, requests accumulate
  indefinitely.
- **Reconciled-action gate** in the binding controller, closing the `kubectl` / GitOps bypass.
- **Notifications.**
- **Portal**, in `openchoreo/backstage-plugins`.
- **Role-based approver resolution** through the PDP. Until then a `roleRef` approver matches nobody,
  so policies must use `entitlement` approvers — or `roleRef` should be rejected at validation.

---

## Appendix

### Reference implementation

A working implementation of the model above accompanies this proposal, covering the CRDs, the authz
actions and registry, the gate, request creation, the REST API and the `occ` command tree. It builds
against the current tree, follows the repo's codegen and audit gates, and includes unit tests for the
gate's security properties — replay prevention, terminal-request handling, suspend, and change
classification.

It is a reference for the design discussion, not a merge candidate: the gaps in *Deferred* above,
and the absence of service- and handler-level tests, are known.

### Prior art

- [Workflow Approvals in Choreo](https://wso2.com/library/blogs/workflow-approvals-choreo/) — the
  feature this adapts. Choreo gates promotion first and describes gated actions as "a growing list".
- [Kargo](https://akuity.io/blog/kargo-gitops-promotion-layer) — prior art for gated promotion that
  stays GitOps-native: freight becomes *eligible*, and a gated promotion is what actuates.

### Key references in the tree

- `internal/authz/core/{actions.go,condition_registry.go}` — action and ABAC model
- `internal/webhook/releasebinding/webhook.go` — the stub validator setting the
  controller-side-validation convention
- `api/v1alpha1/workflowrun_types.go` — the run-instance + TTL shape `ApprovalRequest` follows
- `test/e2e/suites/gitops/` — the Flux path any gate on a reconciled action must stay compatible with
