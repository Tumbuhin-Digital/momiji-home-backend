# Dashboard API Plan
*Based on the Operations Dashboard design (Jun 13, 2026)*

## Overview
The frontend's Operations Dashboard requires a dedicated, aggregated API endpoint (`GET /api/v1/dashboard/summary`) that computes and returns all the stats and data visible on the dashboard in a **single API call**. This avoids N+1 requests from the frontend and keeps the dashboard fast to load.

---

## Dashboard Sections & Data Requirements

### 1. Stat Cards (Top Row)

| Card              | Value Needed                                   | Data Source                  |
|-------------------|------------------------------------------------|------------------------------|
| **Total Products**    | Total distinct product count                  | `products` table             |
| **Available Stock**   | Total sum of `inventory_quantity` > 0 (variant count), delta today (+12) | `product_variants` table     |
| **Orders in Progress**| Count of orders where `aggregate_status = 'on_progress'`, delta today (# today) | `orders` table |
| **Pre-Orders**        | Count of orders containing a `pre_order` item with `item_status = 'pending_deposit'` OR total count of order_line_items of type `pre_order` and status `pending`, delta label "Confirm Pending" | `orders` + `order_line_items` |

---

### 2. Recent Order Queue
A brief list of the **most recent 5 orders**, showing:
- Order number (`#ORD-XXXX`)
- Customer full name (from the `customers` relation)
- Product titles, variant title and quantity summary (first item as preview)
- Order status badge (mapped to a FE-friendly label):
  - `pending_payment` → **"New Order"**
  - `on_progress` (has pre_order items) → **"Pre-Order"**
  - `on_progress` (fully ship_ready) → **"Order Confirm"**

---

### 3. Sales Report

#### Total Revenue
- Sum of `total_price` for all orders in the **current calendar month** where `financial_status = 'paid'`.

#### Monthly Revenue (Bar Chart)
- An aggregation of monthly revenue **for the current year** (Jan–Dec), grouped by the month of `created_at`.
- Returns an array of 12 data points (months with no sales = 0).

---

## Proposed API Contract

### `GET /api/v1/dashboard/summary`
- **Auth:** Admin only (`middleware.Auth` + role check)
- **Response:**

```json
{
  "stat_cards": {
    "total_products": 148,
    "available_stock": {
      "count": 112,
      "delta_today": 12
    },
    "orders_in_progress": {
      "count": 27,
      "delta_today": 8
    },
    "pre_orders": {
      "count": 3,
      "status_label": "Confirm Pending"
    }
  },
  "recent_orders": [
    {
      "order_number": "#ORD-1091",
      "customer_name": "James Bay",
      "items_preview": "3-in-1 Grocery Store, Shop Stall & Study Desk (1pcs)",
      "status": "new_order",
      "status_label": "New Order"
    }
  ],
  "sales_report": {
    "total_revenue_this_month": 1500.00,
    "currency": "USD",
    "monthly_revenue": [
      { "month": "Jan", "revenue": 0 },
      { "month": "Feb", "revenue": 0 },
      { "month": "Mar", "revenue": 0 },
      { "month": "Apr", "revenue": 0 },
      { "month": "May", "revenue": 0 },
      { "month": "Jun", "revenue": 0 },
      { "month": "Jul", "revenue": 1500.00 },
      { "month": "Aug", "revenue": 0 },
      { "month": "Sep", "revenue": 0 },
      { "month": "Oct", "revenue": 0 },
      { "month": "Nov", "revenue": 0 },
      { "month": "Dec", "revenue": 0 }
    ]
  }
}
```

---

## Implementation Plan

### New Package: `internal/dashboard`

#### [NEW] `internal/dashboard/dto.go`
Define all response structs for the dashboard endpoint.

#### [NEW] `internal/dashboard/store.go`
Define the `Store` interface and the raw `DashboardStats` model. Key DB queries needed:
- `COUNT(*) FROM products`
- `COUNT(*) FROM product_variants WHERE inventory_quantity > 0` + `COUNT(*) WHERE DATE(created_at) = TODAY AND inventory_quantity > 0`
- `COUNT(*) FROM orders WHERE aggregate_status = 'on_progress'` + delta today
- `COUNT(DISTINCT order_id) FROM order_line_items WHERE type = 'pre_order' AND item_status IN ('pending_deposit', 'pending')`
- `SELECT created_at, order_number, customer_id, (first item title) FROM orders ORDER BY created_at DESC LIMIT 5` (with Customer preload)
- `SUM(total_price) FROM orders WHERE financial_status = 'paid' AND DATE_TRUNC('month', created_at) = DATE_TRUNC('month', NOW())`
- `SELECT DATE_TRUNC('month', created_at) as month, SUM(total_price) FROM orders WHERE financial_status='paid' AND EXTRACT(year FROM created_at) = EXTRACT(year FROM NOW()) GROUP BY month`

#### [NEW] `internal/dashboard/postgres.go`
Implement the store using **raw SQL** (`db.Raw`) for the aggregation queries. These are complex enough that ORM-based chaining would be messy and unreadable.

#### [NEW] `internal/dashboard/service.go`
Assemble the raw data from the store into the final `DashboardSummary` DTO. Map `aggregate_status` to frontend-friendly status labels.

#### [NEW] `internal/dashboard/handler.go`
Single handler: `GET /dashboard/summary`. Middleware: `Auth` + role must be `admin`.

#### [MODIFY] `cmd/api/main.go` (or wherever routes are registered)
Register the new dashboard handler under `/api/v1/dashboard`.

---

## Status Field Mapping (for `recent_orders`)

| `aggregate_status` | Has pre_order items? | `status` (API)      | `status_label` (display) |
|--------------------|----------------------|---------------------|--------------------------|
| `pending_payment`  | any                  | `new_order`         | New Order                |
| `on_progress`      | yes                  | `pre_order`         | Pre-Order                |
| `on_progress`      | no (all ship_ready)  | `order_confirm`     | Order Confirm            |
| `refunded`         | any                  | `refunded`          | Refunded                 |
| `cancelled`        | any                  | `cancelled`         | Cancelled                |
