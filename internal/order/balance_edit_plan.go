package order

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// moneyTolerance absorbs float noise when comparing amounts that are whole cents.
const moneyTolerance = 0.005

func moneyEqual(a, b float64) bool {
	return math.Abs(a-b) < moneyTolerance
}

// balanceEditLine is one app line item slice billed by a balance edit.
type balanceEditLine struct {
	OrderLineItemID  string
	ShopifyVariantID string
	// DepositLineID is the Shopify line the item was bought on, when stored.
	DepositLineID string
	Title         string
	Units         int
	// TargetUnitPrice is the website price at checkout, which the real line must cost.
	TargetUnitPrice float64
	// AppBalance is what the app expects these units to still owe.
	AppBalance float64
}

type depositReduction struct {
	CalculatedLineID string
	OrderLineItemID  string
	NewQuantity      int
}

type variantAddition struct {
	OrderLineItemID  string
	ShopifyVariantID string
	Title            string
	Units            int
	TargetUnitPrice  float64
}

type shippingAddition struct {
	Title  string
	Amount float64
}

// shippingDepositRemoval takes the checkout's upfront "Shipping (Pre-Order) - 50%" line off
// the order. What the customer paid for it is credited back and billed again inside the
// group's shipping line, so the order shows one full shipping charge like a regular order.
type shippingDepositRemoval struct {
	CalculatedLineID string
	Amount           float64
}

// balanceEditPlan swaps a group's deposit units for the real product at full price.
type balanceEditPlan struct {
	Reductions      []depositReduction
	Additions       []variantAddition
	ShippingDeposit *shippingDepositRemoval
	Shipping        *shippingAddition
	// OutstandingIncrease is how much the edit adds to what the customer owes. Removing a
	// deposit unit takes its paid price off the order total while the payment stays, so the
	// increase per unit is the full price minus the deposit actually charged for it.
	OutstandingIncrease float64
}

func errBalanceEdit(code, message string) error {
	return apierror.New(http.StatusConflict, code, message)
}

const (
	preorderDepositAttrType  = "type"
	preorderDepositAttrValue = "preorder_dp"
	preorderVariantRefAttr   = "variant_ref"
)

func calculatedAttr(line shopify.CalculatedLineItem, key string) string {
	for _, a := range line.CustomAttributes {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

func isDepositCalculatedLine(line shopify.CalculatedLineItem) bool {
	return calculatedAttr(line, preorderDepositAttrType) == preorderDepositAttrValue
}

// shippingDepositCalculatedLine returns the order's live upfront shipping line, or nil when
// there is none or more than one.
func shippingDepositCalculatedLine(calcLines []shopify.CalculatedLineItem) *shopify.CalculatedLineItem {
	var found *shopify.CalculatedLineItem
	for i := range calcLines {
		if calcLines[i].Quantity < 1 || calculatedAttr(calcLines[i], "charge_type") != preorderShippingDepositChargeType {
			continue
		}
		if found != nil {
			return nil
		}
		found = &calcLines[i]
	}
	return found
}

func sameShopifyID(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return shopify.GIDNumericID(a) == shopify.GIDNumericID(b)
}

// findDepositLine picks the deposit line an app line item was bought on: by its stored
// Shopify line ID when known, otherwise by the variant the deposit line references. Two
// candidates for the same variant cannot be told apart safely, so that is refused.
func findDepositLine(line balanceEditLine, calcLines []shopify.CalculatedLineItem) (*shopify.CalculatedLineItem, error) {
	var deposits []shopify.CalculatedLineItem
	for _, cl := range calcLines {
		if isDepositCalculatedLine(cl) {
			deposits = append(deposits, cl)
		}
	}
	if line.DepositLineID != "" {
		for i := range deposits {
			if sameShopifyID(deposits[i].ID, line.DepositLineID) {
				return &deposits[i], nil
			}
		}
	}
	var matches []shopify.CalculatedLineItem
	for _, cl := range deposits {
		if sameShopifyID(calculatedAttr(cl, preorderVariantRefAttr), line.ShopifyVariantID) {
			matches = append(matches, cl)
		}
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, errBalanceEdit("deposit_line_not_found",
			fmt.Sprintf("The deposit line for %s was not found on the Shopify order", line.Title))
	default:
		return nil, errBalanceEdit("deposit_line_ambiguous",
			fmt.Sprintf("The Shopify order has more than one deposit line for %s; edit this order by hand", line.Title))
	}
}

// planBalanceEdit works out the edit that bills a group's balance on the deposit order.
// It reads but never changes anything, so every rule is testable without Shopify.
//
// foldDeposit is the checkout shipping prepayment when this group consumes all of it (0
// otherwise). The upfront shipping line is then removed and its amount added to the
// group's shipping line; the customer owes the same either way.
func planBalanceEdit(lines []balanceEditLine, calcLines []shopify.CalculatedLineItem, remainingShipping float64, shippingTitle string, foldDeposit float64) (*balanceEditPlan, error) {
	plan := &balanceEditPlan{}
	remainingByDeposit := make(map[string]int)
	ownerByDeposit := make(map[string]string)

	for _, line := range lines {
		if line.Units < 1 {
			continue
		}
		if line.TargetUnitPrice <= 0 {
			return nil, errBalanceEdit("balance_price_missing",
				fmt.Sprintf("The checkout price of %s is unknown, so its balance cannot be billed", line.Title))
		}
		deposit, err := findDepositLine(line, calcLines)
		if err != nil {
			return nil, err
		}
		if owner, taken := ownerByDeposit[deposit.ID]; taken && owner != line.OrderLineItemID {
			return nil, errBalanceEdit("deposit_line_ambiguous",
				fmt.Sprintf("Two items share the deposit line for %s; edit this order by hand", line.Title))
		}
		ownerByDeposit[deposit.ID] = line.OrderLineItemID

		left, seen := remainingByDeposit[deposit.ID]
		if !seen {
			left = deposit.Quantity
		}
		if left < line.Units {
			return nil, errBalanceEdit("deposit_line_short",
				fmt.Sprintf("The deposit line for %s only has %d unit(s) left to bill, %d needed", line.Title, left, line.Units))
		}
		remainingByDeposit[deposit.ID] = left - line.Units

		increase := round2((line.TargetUnitPrice - deposit.DiscountedUnitPrice) * float64(line.Units))
		if math.Abs(increase-line.AppBalance) > 0.01+moneyTolerance {
			return nil, errBalanceEdit("balance_mismatch",
				fmt.Sprintf("The balance for %s does not match the Shopify order (app $%.2f, Shopify $%.2f)", line.Title, line.AppBalance, increase))
		}
		plan.OutstandingIncrease += increase
		plan.Additions = append(plan.Additions, variantAddition{
			OrderLineItemID:  line.OrderLineItemID,
			ShopifyVariantID: line.ShopifyVariantID,
			Title:            line.Title,
			Units:            line.Units,
			TargetUnitPrice:  round2(line.TargetUnitPrice),
		})
	}

	for depositID, left := range remainingByDeposit {
		plan.Reductions = append(plan.Reductions, depositReduction{
			CalculatedLineID: depositID,
			OrderLineItemID:  ownerByDeposit[depositID],
			NewQuantity:      left,
		})
	}
	sortReductions(plan.Reductions)

	shipping := round2(remainingShipping)
	if foldDeposit > 0 {
		// Only when the line still holds exactly that prepayment; otherwise it stays.
		if line := shippingDepositCalculatedLine(calcLines); line != nil &&
			moneyEqual(round2(line.DiscountedUnitPrice*float64(line.Quantity)), foldDeposit) {
			amount := round2(foldDeposit)
			plan.ShippingDeposit = &shippingDepositRemoval{CalculatedLineID: line.ID, Amount: amount}
			plan.OutstandingIncrease -= amount
			shipping = round2(shipping + amount)
		}
	}
	if shipping > 0 {
		title := strings.TrimSpace(shippingTitle)
		if title == "" {
			title = "Shipping & delivery"
		}
		plan.Shipping = &shippingAddition{Title: title, Amount: shipping}
		plan.OutstandingIncrease += shipping
	}
	plan.OutstandingIncrease = round2(plan.OutstandingIncrease)

	if len(plan.Additions) == 0 && plan.Shipping == nil {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Nothing to invoice for this group")
	}
	return plan, nil
}

// sortReductions keeps the edit order stable so logs and tests are predictable.
func sortReductions(r []depositReduction) {
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && r[j].CalculatedLineID < r[j-1].CalculatedLineID; j-- {
			r[j], r[j-1] = r[j-1], r[j]
		}
	}
}

// matchBalanceLines links the real product lines a committed edit added to the app line
// items they bill. known holds Shopify line IDs that existed before the edit (or are known
// to belong to something else); every other live line of a billed variant is a candidate.
// An exact unit match is preferred so two slices of one variant land on the right lines.
func matchBalanceLines(after []shopify.OrderLineState, known map[string]bool, additions []variantAddition) ([]PreorderBalanceLine, error) {
	var candidates []shopify.OrderLineState
	for _, l := range after {
		if l.VariantID == "" || l.CurrentQuantity < 1 || known[shopify.GIDNumericID(l.ID)] {
			continue
		}
		candidates = append(candidates, l)
	}
	used := make(map[string]bool)
	out := make([]PreorderBalanceLine, 0, len(additions))
	for _, add := range additions {
		pick := -1
		for i, c := range candidates {
			if used[c.ID] || !sameShopifyID(c.VariantID, add.ShopifyVariantID) {
				continue
			}
			if c.CurrentQuantity == add.Units {
				pick = i
				break
			}
			if pick == -1 {
				pick = i
			}
		}
		if pick == -1 {
			return out, fmt.Errorf("no new Shopify line found for %s", add.Title)
		}
		used[candidates[pick].ID] = true
		out = append(out, PreorderBalanceLine{
			OrderLineItemID:   add.OrderLineItemID,
			ShopifyLineItemID: candidates[pick].ID,
			Quantity:          add.Units,
			UnitPrice:         add.TargetUnitPrice,
		})
	}
	return out, nil
}

// findShippingLine returns the live shipping line with the given ID, falling back to a
// unique title match for lines whose ID was never stored.
func findShippingLine(lines []shopify.CalculatedShippingLine, storedID, title string) *shopify.CalculatedShippingLine {
	live := func(l shopify.CalculatedShippingLine) bool {
		return !strings.EqualFold(l.StagedStatus, "REMOVED")
	}
	if storedID != "" {
		for i := range lines {
			if live(lines[i]) && sameShopifyID(lines[i].ID, storedID) {
				return &lines[i]
			}
		}
	}
	var match *shopify.CalculatedShippingLine
	for i := range lines {
		if !live(lines[i]) || !strings.EqualFold(strings.TrimSpace(lines[i].Title), strings.TrimSpace(title)) {
			continue
		}
		if match != nil {
			return nil
		}
		match = &lines[i]
	}
	return match
}

// newShippingLineID returns the shipping line present after an edit but not before it.
func newShippingLineID(before, after []shopify.OrderShippingLineState, title string) string {
	existed := make(map[string]bool, len(before))
	for _, l := range before {
		existed[shopify.GIDNumericID(l.ID)] = true
	}
	id := ""
	for _, l := range after {
		if existed[shopify.GIDNumericID(l.ID)] {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(l.Title), strings.TrimSpace(title)) {
			return l.ID
		}
		if id == "" {
			id = l.ID
		}
	}
	return id
}
