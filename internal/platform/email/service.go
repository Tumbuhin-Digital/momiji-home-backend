package email

import (
	"bytes"
	"context"
	"html/template"
	"path/filepath"
)

type NotificationService interface {
	SendOrderConfirmation(ctx context.Context, to string, data OrderEmailData) error
	SendInvoice(ctx context.Context, to string, data SettlementEmailData) error
	SendSettlementPaid(ctx context.Context, to string, data SettlementEmailData) error
	SendReminder(ctx context.Context, to string, data SettlementEmailData) error
	SendExpired(ctx context.Context, to string, data SettlementEmailData) error
	SendShipmentDispatched(ctx context.Context, to string, data ShipmentEmailData) error
}

type OrderEmailData struct {
	CustomerName    string
	OrderNumber     string
	Items           []OrderItemData
	TotalPaid       string
	HasBalanceDue   bool
	TotalBalanceDue string
}

type OrderItemData struct {
	Title    string
	Type     string
	Quantity int
	Amount   string
}

type SettlementItemData struct {
	Title  string
	Amount string
}

type SettlementEmailData struct {
	CustomerName   string
	ItemTitle      string
	Items          []SettlementItemData
	BalanceAmount  string
	ShippingTitle  string
	ShippingAmount string
	TotalDue       string
	ShippingNotes  string
	DueDate        string
	PaymentLink    string
}

type ShipmentEmailData struct {
	CustomerName   string
	OrderNumber    string
	Carrier        string
	TrackingNumber string
	TrackingURL    string
}

type service struct {
	client       EmailClient
	templatesDir string
}

func NewNotificationService(client EmailClient, templatesDir string) NotificationService {
	return &service{
		client:       client,
		templatesDir: templatesDir,
	}
}

func (s *service) render(name string, data interface{}) (string, error) {
	path := filepath.Join(s.templatesDir, name)
	t, err := template.ParseFiles(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *service) SendOrderConfirmation(ctx context.Context, to string, data OrderEmailData) error {
	html, err := s.render("order_confirmation.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Order Confirmation - " + data.OrderNumber, html)
}

func (s *service) SendInvoice(ctx context.Context, to string, data SettlementEmailData) error {
	html, err := s.render("settlement_invoice.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Pre-order Balance Invoice - " + data.ItemTitle, html)
}

func (s *service) SendSettlementPaid(ctx context.Context, to string, data SettlementEmailData) error {
	html, err := s.render("settlement_paid.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Payment Received - " + data.ItemTitle, html)
}

func (s *service) SendReminder(ctx context.Context, to string, data SettlementEmailData) error {
	html, err := s.render("settlement_reminder.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Payment Reminder - " + data.ItemTitle, html)
}

func (s *service) SendExpired(ctx context.Context, to string, data SettlementEmailData) error {
	html, err := s.render("settlement_expired.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Pre-order Canceled - " + data.ItemTitle, html)
}

func (s *service) SendShipmentDispatched(ctx context.Context, to string, data ShipmentEmailData) error {
	html, err := s.render("shipment_dispatched.html", data)
	if err != nil {
		return err
	}
	return s.client.Send(ctx, []string{to}, "Order Shipped - " + data.OrderNumber, html)
}
