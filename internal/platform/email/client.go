package email

import "context"

// EmailClient defines the interface for sending emails.
type EmailClient interface {
	Send(ctx context.Context, to []string, subject, htmlBody string) error
}
