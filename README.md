# Refer & Earn — Code Review (branch `refer-and-earn`)

> Reviewed: 2026-07-08. Scope: the two `Refer & Earn:` commits (`8c986977`,
> `33f63265`) and everything they touch. The branch also carries an unrelated
> chain of Facebook lead-sync commits inherited from an earlier merge — those
> are not part of this feature and are not covered here.

## What's actually on the branch today

The design in `docs/refer-and-earn-design.md` (v1: in-CRM wallet, gift cards,
public referrer hub) was **superseded** by `docs/integration/crm-affiliate-contract.md`
partway through the branch. Commit `33f63265` deliberately reverted the public
hub/landing pages and the CRM-side "copy referral link" feature and replaced
them with a thin event-emitter model:

- A **standalone affiliate app** now owns ambassadors, referral codes, the
  public referrer/referee pages, and all reward math.
- The CRM's job is reduced to two seams:
  1. **Inbound**: `POST /integrations/affiliate/lead` receives leads captured
     on the affiliate site and drops them into a new "Affiliate-Leads" tab
     (`AffiliateLeadReceiverController`, `ReferralLeadsController`).
  2. **Outbound**: when a counsellor marks one of those leads Qualified /
     Not-qualified, an outbox row is written and a queued job POSTs a
     `lead_qualified` / `lead_rejected` event to the affiliate app
     (`ReferralEventEmitter` → `referral_outbox` → `DispatchReferralEvent`).

This is a sound shape — the isolation principle (new tables, soft `lead_id`
references, everything gated behind `$referral_view`) is followed consistently,
and the affiliate integration correctly keeps money logic out of the CRM. The
findings below are gaps between that intent and what's actually wired up,
roughly in priority order.

---

## Findings

### 1. Retry/backoff on the outbox likely doesn't work in production
`DispatchReferralEvent` declares `$tries = 6` and a `backoff()` schedule
(1m/5m/30m/2h/6h), but `.env.example` ships `QUEUE_CONNECTION=sync` and
neither `docker-compose.yml` nor `Kernel.php` runs a queue worker or a
scheduled sweep of pending/failed rows. Under the `sync` driver the job runs
inline inside the HTTP request, and any thrown exception is swallowed by
`ReferralEventEmitter::emit()`'s own `try/catch` — the row is left `pending`
with nothing that will ever retry it. The contract's core promise ("a
slow/down affiliate app can never break or delay a status change... outbox +
queued job with retries") only holds if a real queue connection + worker is
configured in prod. Worth confirming that's the case outside this repo; if
not, this needs either a proper queue or a scheduled command that sweeps
stale `pending` rows.

**Files:** `app/Jobs/Referral/DispatchReferralEvent.php`, `app/Console/Kernel.php`, `.env.example`, `docker-compose.yml`

### 2. No replay path for permanently-failed events
`docs/integration/crm-affiliate-contract.md` §5 promises "a `failed` row can
be manually **replayed**". There's no command, route, or UI anywhere that
does this — a row that hits `STATUS_FAILED` (4xx payload error, or 6
exhausted retries) has no recovery short of manual DB writes. Low effort to
add (an artisan command or an admin button that resets `status`/`attempts`
and re-dispatches), but currently missing.

### 3. Affiliate-Leads nav/role gate is a no-op
`resources/views/v3/sidebar/menulist.blade.php:399` guards the tab with
`userHasAnyRole($user_type)`. Elsewhere in the codebase `$user_type` is set to
`Auth::user()->role_names` — i.e. **the current user's own roles** — so the
call reduces to `userHasAnyRole($currentUser->role_names)`, which is
`array_intersect($currentUser->role_names, $currentUser->role_names)`: true
for any authenticated user with at least one role. Compare with the "All
Leads" item right above it, which is properly gated by
`in_array('lead.all', $menu_items)`. The route itself
(`/referral-ambassador`, `routes/web.php:311`) also only requires
`auth`+`2fa`, no role middleware. Net effect: every logged-in user sees and
can open the Affiliate-Leads tab, not the "role + branch scoped" access the
design doc calls for. The underlying data is still scoped by the shared
`leads_data()` query (same as All Leads), so this isn't a data leak beyond
what a user could already see — but the intended restriction isn't there,
and the code reads as if it is.

**File:** `resources/views/v3/sidebar/menulist.blade.php:399`

### 4. No dedupe / self-referral guard on inbound capture
`AffiliateLeadReceiverController::capture()` unconditionally creates a new
`Lead` (+ `ReferralLead`) row on every POST — there's no lookup by
email/phone against existing leads. The design doc's fraud rules
("self-referral blocked", "existing-lead referee flagged for review, not
auto-credited") aren't enforced CRM-side. If the affiliate app retries a
failed POST, or the same person is referred twice, the CRM will happily
create duplicate lead records. This may be intentionally punted to the
affiliate app (which owns the reward decision), but the CRM's own data
quality is still affected regardless of who decides the reward — worth
confirming this is accepted, since dedupe is cheap to add (match on
`email` OR `phone` before creating).

**File:** `app/Http/Controllers/Referral/AffiliateLeadReceiverController.php:70-140`

### 5. Orphaned schema from the abandoned v1 design
The `referrers` and `referral_settings` migrations/models (from commit
`8c986977`, the in-CRM-wallet design) survived the pivot in `33f63265` even
though nothing creates or reads them anymore:
- No listener on `StudentEnrolled` (`EventServiceProvider` only wires
  `GenerateCommissionRecords` to it) — the `Referrer::generateUniqueCode()` /
  `generateUniqueToken()` helpers are dead code.
- No model/controller touches `referral_settings` at all.
- `ReferralLead::referrer()` and `ReferralLeadsController` still join against
  `referrers`, but the relation will always resolve to `null` in the current
  flow (referrer identity now lives in the affiliate app; `referrer_name` on
  `referral_leads` is the actual display source, per the `2026_07_06` migration).

Recommend dropping `referrers` + `referral_settings` (table, migration, model)
unless there's a concrete near-term plan to use them — as-is they're
confusing artifacts of a design that was explicitly superseded.

**Files:** `app/Models/Referrer.php`, `database/migrations/2026_06_18_000001_*`, `database/migrations/2026_06_18_000002_*`

### 6. `referral_leads.status` is a dead column
Migration comment says it "drives rewards later"; it's set to `'new'` on
creation and never updated anywhere else in the codebase. Harmless today, but
misleading — a future reader will reasonably assume it reflects qualified/
enrolled state when it doesn't. Either wire it up (update it from
`ReferralEventEmitter::emit()`, which already knows the event type) or drop
it since the affiliate app is the actual source of truth for lead lifecycle.

### 7. No test coverage for the new flows
`FacebookWebhookTest.php` and `AuthLoginTest.php` were added on this branch,
but nothing exercises the referral-specific logic: service-key auth on the
inbound endpoints, the idempotency/re-dispatch behavior in
`ReferralEventEmitter::emit()` (particularly the "decision events re-deliver
on flip-flop, fact events don't" branch), or the 4xx-vs-5xx/401 retry
branching in `DispatchReferralEvent`. These are exactly the kind of
conditional logic that's easy to regress silently.

### 8. Minor — fragile (but currently correct) ping/save race
In `all_lead.blade.php`'s `saveThenPing()`, the status-update form submit and
the reward-event AJAX ping are fired back-to-back without waiting for the
save's response. It's safe today because `ReferralEventEmitter::emit()` only
checks that a `ReferralLead` row exists for the lead id, not the lead's
persisted status — but if that emitter is ever tightened to validate current
status, this ordering will silently break. Worth a comment noting the
coupling, or sequencing the ping after the save's callback.

---

## What's solid

- **Isolation discipline is real, not just a comment.** New tables, soft
  `lead_id` references (no FKs into `leads`), and every shared-view change
  gated behind `$referral_view` — All Leads is verifiably untouched.
- **Service-key comparison uses `hash_equals`** (timing-safe) in both the
  inbound guard and would be expected outbound — good instinct.
- **The emitter never breaks the caller**: every write/dispatch path in
  `ReferralEventEmitter` is wrapped so a delivery hiccup can't surface as an
  error to a counsellor clicking "Qualified".
- **Idempotency is handled thoughtfully**: unique `(event, external_lead_id)`
  index, plus the deliberate distinction between one-shot "fact" events
  (submitted/enrolled — duplicate ignored) and re-issuable "decision" events
  (qualified/rejected — re-delivered on flip-flop). That's a subtle design
  point and it's implemented correctly.
- **4xx vs 401/5xx retry branching** in `DispatchReferralEvent` correctly
  treats permanent payload errors differently from transient/auth errors.
- The Affiliate-Leads tab reusing the All-Leads view/query instead of forking
  a parallel implementation is a good low-risk choice — behavior parity is
  guaranteed "by construction" rather than by careful duplication.

---

## Suggested next steps (roughly by priority)

1. Confirm production `QUEUE_CONNECTION` + worker setup for the outbox job
   (#1) — this is the one that silently defeats the reliability guarantee
   the contract is built around.
2. Add a replay mechanism for `failed` outbox rows (#2).
3. Fix or remove the `userHasAnyRole($user_type)` nav gate (#3) — either wire
   real role/permission scoping or drop the pretense.
4. Decide whether CRM-side dedupe is in scope (#4), and drop or repurpose the
   orphaned `referrers`/`referral_settings` schema (#5).
5. Add feature tests around `ReferralEventEmitter` and the inbound receiver
   before this goes further (#7) — the idempotency/retry logic is exactly
   where regressions hide.
