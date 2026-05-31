# Phase 8: Email Notifications

> **Status:** 🔲 PENDING
> **Date:** 2026-05-31
> **Pre-requisite:** All API endpoints implemented and wired ✅

---

## Current State: What's Fully Done

All API contract endpoints are now implemented and mounted:
- ✅ Auth (login, logout, refresh, me)
- ✅ Cart (session, get, summary, add, update, remove, clear, merge)
- ✅ Checkout (shipping methods, calculate, checkout summary)
- ✅ Products (list paginated, get by ID, variants, sync, update status/batch/price)
- ✅ Orders (create, list, get, accept, cancel, step, received)
- ✅ Preorders (list, get settlement, invoice, mark paid)
- ✅ Customers (list, get, get orders)

**Remaining from PRD:** Email notifications, Shopify webhooks, and reporting. Email notifications are the highest-impact next step — they are required for the pre-order fulfillment flow to work end-to-end.

---

## Business Rules for Email (from PRD)

| Trigger Event | Recipient | Email Content |
|--------------|-----------|---------------|
| Order created (paid) | Customer | Order confirmation, itemized receipt with ship_ready + pre_order breakdown |
| Settlement invoiced (Accept Order) | Customer | Balance invoice link + amount due + due date |
| Settlement paid (Pelunasan received) | Customer | Payment confirmed, preparing shipment 2 |
| Pre-order reminder D+3 (3 days after invoice) | Customer | Reminder: balance still outstanding |
| Pre-order reminder D+6 | Customer | Final reminder: expires tomorrow, refund policy warning |
| Settlement expired D+7 (no payment) | Customer | Expired, 80% refund processed |
| Shipment dispatched | Customer | Tracking number, carrier, estimated delivery |

---

## Architecture Decision: Email Provider

**Using: Go standard library SMTP (`net/smtp`) + `gopkg.in/gomail.v2`**

No third-party email API service is used at this stage. We send emails directly via SMTP using a configured mail server (e.g., Gmail SMTP, company SMTP, or a local SMTP relay like Mailpit for dev).

`gomail.v2` is the chosen library because it handles MIME multipart (HTML body + plain text fallback), attachments, and TLS/STARTTLS in a clean API on top of `net/smtp` — avoiding the tedious manual MIME formatting.

> **Note:** The `EmailClient` interface abstraction is still kept. When a real email service provider is added later (Resend, SendGrid, SES), only the SMTP implementation file needs to be swapped — no business logic changes required.

**Email provider is configured via `.env`:**
```
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=noreply@momiji-home.com
SMTP_PASSWORD=your_app_password
EMAIL_FROM=noreply@momiji-home.com
```

---

## Proposed Changes

### Part 0 — Email Platform Layer

#### [NEW] `internal/platform/email/`
- `client.go` — defines `EmailClient` interface: `Send(ctx, to, subject, htmlBody string) error`
- `smtp.go` — `SMTPClient` implementation using `net/smtp` + `gopkg.in/gomail.v2`. Reads SMTP credentials from config, dials TLS, sends the email.
- `mock.go` — `MockEmailClient` for unit tests, captures sent emails in-memory without making any network calls.

This follows the same pattern as `internal/platform/shopify/` — an interface + production impl + test mock.

**For local development**, use [Mailpit](https://github.com/axllent/mailpit) (runs via Docker) as a local SMTP trap. It catches all emails without sending them and provides a web UI to inspect them:
```yaml
# docker-compose.yml addition
mailpit:
  image: axllent/mailpit
  ports:
    - "1025:1025"  # SMTP
    - "8025:8025"  # Web UI
```
Set `SMTP_HOST=localhost SMTP_PORT=1025` in dev `.env`.

---

### Part 1 — Email Templates

#### [NEW] `internal/platform/email/templates/`
Plain HTML files or Go `text/template` strings for each email type:
- `order_confirmation.html`
- `settlement_invoice.html`
- `settlement_paid.html`
- `settlement_reminder.html`
- `settlement_expired.html`
- `shipment_dispatched.html`

Templates use Go's `text/template` engine. No external dependency needed.

---

### Part 2 — Email Service

#### [NEW] `internal/platform/email/service.go`
A `NotificationService` that wraps `EmailClient` and exposes typed methods:
- `SendOrderConfirmation(ctx, to string, order OrderEmailData) error`
- `SendInvoice(ctx, to string, settlement SettlementEmailData) error`
- `SendSettlementPaid(ctx, to string, settlement SettlementEmailData) error`
- `SendReminder(ctx, to string, settlement SettlementEmailData, day int) error`
- `SendExpired(ctx, to string, settlement SettlementEmailData) error`
- `SendShipmentDispatched(ctx, to string, shipment ShipmentEmailData) error`

Each method renders the appropriate template and calls `EmailClient.Send`.

---

### Part 3 — Inject at Trigger Points

The `NotificationService` is injected into the relevant modules:

**`internal/order/service.go`**
- After `CreateOrder` succeeds → fire `SendOrderConfirmation`

**`internal/preorder/service.go`**
- After `InvoiceSettlement` → fire `SendInvoice`
- After `MarkSettlementPaid` → fire `SendSettlementPaid`

**`cmd/server/main.go`**
- Initialize `emailClient`, `notificationService`
- Inject into `orderService` and `preorderService`

---

### Part 4 — Scheduled Reminder Jobs (D+3, D+6)

The PRD requires automatic reminders sent 3 and 6 days after a settlement is invoiced.

**Approach: Background goroutine with a daily ticker**
- On server startup, a goroutine runs a daily job at a fixed time (e.g., midnight UTC)
- The job queries `preorder_settlements` WHERE `status = 'invoiced'` AND `invoiced_at` is 3 or 6 days ago
- For each matching settlement, it fetches the customer email and fires the appropriate reminder

#### [NEW] `internal/platform/scheduler/scheduler.go`
- `StartDailyJob(ctx, fn func())` — launches a goroutine with `time.NewTicker(24 * time.Hour)`
- Called from `cmd/server/main.go` at startup

#### [MODIFY] `internal/preorder/service.go`
- Add `GetSettlementsForReminder(ctx, daysSinceInvoiced int) ([]Settlement, error)` to support the daily job

---

## Verification Plan

```bash
go build ./...
go test ./internal/platform/email/... -v
go test ./internal/order/... -v
go test ./internal/preorder/... -v
```

### Manual E2E
```
1. POST /orders   → check inbox for order confirmation email
2. PATCH /preorders/settlements/:id/invoice → check inbox for invoice email
3. PATCH /preorders/settlements/:id/paid   → check inbox for settlement paid email
4. Manually set invoiced_at = NOW() - INTERVAL '3 days' in DB
   → trigger daily job manually → check inbox for D+3 reminder
```

> Use a service like Mailtrap or Resend's built-in test mode for local development to avoid sending real emails.
