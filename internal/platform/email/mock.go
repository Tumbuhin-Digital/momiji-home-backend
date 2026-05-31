package email

import "context"

type MockEmailClient struct {
	SentEmails []MockEmail
}

type MockEmail struct {
	To       []string
	Subject  string
	HTMLBody string
}

func NewMockEmailClient() *MockEmailClient {
	return &MockEmailClient{
		SentEmails: make([]MockEmail, 0),
	}
}

func (m *MockEmailClient) Send(ctx context.Context, to []string, subject, htmlBody string) error {
	m.SentEmails = append(m.SentEmails, MockEmail{
		To:       to,
		Subject:  subject,
		HTMLBody: htmlBody,
	})
	return nil
}
