// Package mail reads the mail relay's activity and sends the console's test
// email.
//
// The relay (github.com/yundera/mail-gateway, container `smtp`) accepts SMTP
// from apps on the pcs network and forwards each message to mesh-router-backend,
// which sends it from <app>.<domainName>@<serverDomain>. From 1.1.0 it keeps
// counters on its own volume and prints them with `mail-gateway stats`; the
// console reads them through `docker exec`, so no stats port is exposed on pcs.
package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// LimitPerHour mirrors RATE_LIMIT_MAX in mesh-router-backend
// src/services/Email.ts. Keep them in step.
const LimitPerHour = 100

// ErrUnsupported means the relay is older than 1.1.0 and keeps no statistics.
var ErrUnsupported = errors.New("this mail relay version does not report activity; it is added by the next update")

// Counts is one window of the relay's totals.
type Counts struct {
	Sent        int `json:"sent"`
	Failed      int `json:"failed"`
	Skipped     int `json:"skipped"`
	RateLimited int `json:"rateLimited"`
}

type App struct {
	App     string `json:"app"`
	From    string `json:"from,omitempty"`
	Sent    int    `json:"sent"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	// RateLimited counts messages the backend refused over the hourly limit.
	RateLimited int        `json:"rateLimited"`
	Last        *time.Time `json:"last,omitempty"`
}

type Event struct {
	Time   time.Time `json:"time"`
	App    string    `json:"app"`
	From   string    `json:"from,omitempty"`
	To     string    `json:"to"`
	Status string    `json:"status"` // sent | skipped | failed | rate_limited
	Error  string    `json:"error,omitempty"`
}

// Stats is `mail-gateway stats`'s output.
type Stats struct {
	Version string     `json:"version"`
	Since   *time.Time `json:"since,omitempty"`
	Totals  struct {
		H24 Counts `json:"h24"`
		D7  Counts `json:"d7"`
	} `json:"totals"`
	Apps       []App   `json:"apps"`
	Recent     []Event `json:"recent"` // newest first
	Persistent bool    `json:"persistent"`
}

// Execer is the slice of dockerx.Client this package needs.
type Execer interface {
	Exec(ctx context.Context, name string, argv []string) (string, error)
}

// ReadStats asks the relay container for its counters.
func ReadStats(ctx context.Context, d Execer, container string) (*Stats, error) {
	out, err := d.Exec(ctx, container, []string{"/app/mail-gateway", "stats"})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "No such container") || strings.Contains(msg, "is not running") {
			return nil, fmt.Errorf("the mail relay container %q is not running", container)
		}
		// 1.1.0+ recognises the argument and names its file when it cannot read it.
		if strings.Contains(msg, "cannot read") {
			return nil, fmt.Errorf("the mail relay could not read its activity file: %s", msg)
		}
		// 1.0.x ignores the argument and starts a second server, which fails on
		// its missing env or busy port: any failure there means "too old".
		return nil, ErrUnsupported
	}
	return ParseStats(out)
}

func ParseStats(out string) (*Stats, error) {
	var s Stats
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &s); err != nil {
		return nil, ErrUnsupported
	}
	return &s, nil
}

// Sender sends the test email. An interface so handlers can be tested without
// dialling.
type Sender interface {
	Send(ctx context.Context, to string) error
}

// SMTPSender talks plain SMTP to the relay over the pcs network. The relay
// takes the app name from the envelope sender's local part, so this mail goes
// out as mesh-console.<domainName>@<serverDomain>.
type SMTPSender struct {
	Addr string // host:port, e.g. smtp:587
}

const testFrom = "mesh-console@localhost"

func (s SMTPSender) Send(ctx context.Context, to string) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("cannot reach the mail relay at %s: %w", s.Addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	host, _, _ := net.SplitHostPort(s.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail relay: %w", err)
	}
	defer c.Close()
	if err := c.Mail(testFrom); err != nil {
		return fmt.Errorf("mail relay refused the sender: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mail relay refused the recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail relay: %w", err)
	}
	if _, err := w.Write([]byte(testMessage(to))); err != nil {
		return fmt.Errorf("mail relay: %w", err)
	}
	// Close is where the relay answers with the backend's verdict.
	if err := w.Close(); err != nil {
		return fmt.Errorf("the email was not delivered: %w", err)
	}
	return c.Quit()
}

func testMessage(to string) string {
	body := "This is a test email from your Mesh Console.\r\n\r\n" +
		"If you are reading it, the apps on your server can send email.\r\n"
	return "From: Mesh Console <" + testFrom + ">\r\n" +
		"To: " + to + "\r\n" +
		"Subject: Test email from your Mesh Console\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body
}
