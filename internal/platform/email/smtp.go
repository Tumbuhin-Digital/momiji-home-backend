package email

import (
	"context"

	"gopkg.in/gomail.v2"
)

type smtpClient struct {
	host     string
	port     int
	user     string
	password string
	from     string
}

func NewSMTPClient(host string, port int, user, password, from string) EmailClient {
	return &smtpClient{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
	}
}

func (c *smtpClient) Send(ctx context.Context, to []string, subject, htmlBody string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", c.from)
	m.SetHeader("To", to...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	d := gomail.NewDialer(c.host, c.port, c.user, c.password)
	
	// Send emails
	return d.DialAndSend(m)
}
