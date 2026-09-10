// Package mail sends the two messages Phase 1 needs: an invitation and a
// password reset (P1-19, ADR-018).
//
// SMTP and nothing else. No provider SDK, because an SDK is a vendor
// dependency in code rather than in configuration — and for an identity
// provider, where the alternative to "our email provider is down" is "nobody
// can reset a password", keeping that switch cheap is worth more than a
// deliverability dashboard.
//
// **Nothing in this package decides the outcome of a request.** A send that
// fails is logged and counted; the invitation still returns 201 with
// invite_email_sent: false, and a password reset returns the same 202 it
// returns for an address that does not exist. Anything else would make a
// delivery failure into an enumeration oracle.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// ErrNotConfigured means no SMTP endpoint was configured.
//
// Returned rather than silently succeeding: a caller that logs "sent" for a
// deployment with no mail server is a caller telling an administrator their
// invitation is on its way when nothing left the building.
var ErrNotConfigured = errors.New("mail: no SMTP endpoint is configured")

// Message is one outbound email.
//
// Text only. An identity provider's mail is a sentence and a link, and an HTML
// part would add a rendering surface, a second copy of every string to keep in
// step, and the visual vocabulary that phishing depends on. A plain message
// that says what it is and shows the URL in full is easier for a recipient to
// judge than a styled one.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a message.
//
// An interface so a test can assert what would have been sent without a mail
// server, and so a deployment with none can substitute something honest.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Observer counts what goes wrong.
type Observer interface {
	// MailSendFailed counts a message that could not be delivered. ADR-018
	// makes this the metric that keeps a best-effort send safe: without it,
	// an outage looks exactly like nothing happening.
	MailSendFailed(reason string)
}

// SMTP sends over SMTP with STARTTLS.
type SMTP struct {
	Addr string // host:port
	From string

	// Auth is nil when the relay takes no credentials, which is the normal
	// case for a local Mailpit and for a relay bound to a private network.
	Auth smtp.Auth

	// TLSConfig is used for STARTTLS. Never nil in practice; a nil one would
	// take Go's defaults, which verify the certificate, which is what we want
	// anyway — this field exists so a test can point at a local server.
	TLSConfig *tls.Config

	// AllowPlaintext permits sending to a server that offers no STARTTLS.
	//
	// False by default and it must stay that way outside development: an
	// invitation link and a password-reset link are bearer credentials for an
	// account, and sending them over a cleartext hop hands them to anything on
	// the path. Mailpit offers no TLS, which is exactly why this is explicit
	// rather than inferred from the address being localhost — "it is
	// localhost" is a judgement about deployment that the code cannot make.
	AllowPlaintext bool

	Timeout time.Duration
	Log     *slog.Logger
	Metrics Observer
}

// FromURL builds a sender from a configuration URL.
//
//	smtp://localhost:1025                     — no auth, no TLS (development)
//	smtps://user:pass@smtp.example.com:587     — STARTTLS, PLAIN auth
//
// An empty URL returns nil, which is not an error: a deployment that sends no
// mail is a valid one, and it says so at startup rather than at the first
// invitation.
func FromURL(raw, from string, log *slog.Logger, metrics Observer) (*SMTP, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mail: parsing the SMTP URL: %w", err)
	}

	switch u.Scheme {
	case "smtp", "smtps":
	default:
		return nil, fmt.Errorf("mail: %q is not an SMTP URL (want smtp:// or smtps://)", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("mail: the SMTP URL has no host")
	}
	if strings.TrimSpace(from) == "" {
		return nil, fmt.Errorf("mail: a From address is required when SMTP is configured")
	}

	addr := u.Host
	if u.Port() == "" {
		if u.Scheme == "smtps" {
			addr = net.JoinHostPort(u.Hostname(), "587")
		} else {
			addr = net.JoinHostPort(u.Hostname(), "25")
		}
	}

	sender := &SMTP{
		Addr:      addr,
		From:      from,
		TLSConfig: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12},
		// smtp:// is the development scheme and says so by being the one that
		// tolerates a server with no TLS.
		AllowPlaintext: u.Scheme == "smtp",
		Timeout:        10 * time.Second,
		Log:            log,
		Metrics:        metrics,
	}

	if u.User != nil {
		password, _ := u.User.Password()
		sender.Auth = smtp.PlainAuth("", u.User.Username(), password, u.Hostname())
	}

	return sender, nil
}

// Send delivers one message.
//
// The context bounds the dial; net/smtp has no context support of its own, so
// the deadline is applied to the connection and the rest of the exchange runs
// under the connection's own timeout.
func (s *SMTP) Send(ctx context.Context, msg Message) error {
	if s == nil || s.Addr == "" {
		return ErrNotConfigured
	}

	dialer := &net.Dialer{Timeout: s.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return s.failed("dial", fmt.Errorf("mail: connecting to %s: %w", s.Addr, err))
	}
	defer func() { _ = conn.Close() }()

	if s.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(s.Timeout))
	}

	host, _, _ := net.SplitHostPort(s.Addr)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return s.failed("handshake", fmt.Errorf("mail: SMTP handshake: %w", err))
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(s.TLSConfig); err != nil {
			return s.failed("starttls", fmt.Errorf("mail: STARTTLS: %w", err))
		}
	} else if !s.AllowPlaintext {
		// The link in this message is a bearer credential for an account.
		// Refusing is the only safe answer; sending anyway would hand it to
		// anything on the path.
		return s.failed("no_tls", fmt.Errorf(
			"mail: %s offers no STARTTLS and plaintext is not permitted; "+
				"an invitation or reset link is a credential", s.Addr))
	}

	if s.Auth != nil {
		if err := client.Auth(s.Auth); err != nil {
			return s.failed("auth", fmt.Errorf("mail: authenticating: %w", err))
		}
	}

	if err := client.Mail(s.From); err != nil {
		return s.failed("from", fmt.Errorf("mail: MAIL FROM: %w", err))
	}
	if err := client.Rcpt(msg.To); err != nil {
		// Deliberately not distinguished from any other failure by the caller.
		// A rejected recipient is exactly the signal an enumeration attack
		// wants, so it must not change what the API answers.
		return s.failed("recipient", fmt.Errorf("mail: RCPT TO: %w", err))
	}

	w, err := client.Data()
	if err != nil {
		return s.failed("data", fmt.Errorf("mail: DATA: %w", err))
	}
	if _, err := w.Write([]byte(compose(s.From, msg))); err != nil {
		return s.failed("write", fmt.Errorf("mail: writing the message: %w", err))
	}
	if err := w.Close(); err != nil {
		return s.failed("close", fmt.Errorf("mail: closing the message: %w", err))
	}

	return client.Quit()
}

func (s *SMTP) failed(reason string, err error) error {
	if s.Metrics != nil {
		s.Metrics.MailSendFailed(reason)
	}
	if s.Log != nil {
		// The recipient is NOT logged. docs/PLAN/13 keeps addresses out of
		// logs, and a failure log listing every address a reset was requested
		// for would be an enumeration list sitting in the log store.
		s.Log.Warn("could not send an email", "reason", reason, "error", err.Error())
	}
	return err
}

// compose builds the RFC 5322 message.
//
// The subject is encoded rather than written raw: a non-ASCII organization
// name in a subject line is ordinary, and an unencoded one arrives as mojibake
// or gets the message rejected.
func compose(from string, msg Message) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	// An automated message should not generate an out-of-office reply, and a
	// reset notification bouncing off a vacation responder to a mailing list
	// is a real way for a link to reach somewhere it should not.
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	b.WriteString(normalise(msg.Body))
	return b.String()
}

// normalise makes line endings CRLF and escapes a leading dot.
//
// A line consisting of a single "." ends the DATA phase. Body text containing
// one would truncate the message and leave the rest to be interpreted as SMTP
// commands, which is a message-injection bug rather than a formatting one.
func normalise(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}
