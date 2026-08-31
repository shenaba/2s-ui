package notify

import (
	"crypto/tls"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/util/common"
)

// smtpTimeout bounds one whole message: the dial, the TLS handshake, the auth
// exchange and every command after it.
//
// net/smtp's own Dial has no timeout and sets no deadline, and neither does
// tls.Dial, so without this a host that accepts the connection and then stops
// answering blocks the sender forever. That is not one lost alert: it wedges
// the "smtp" subscriber's worker, whose 64-slot queue then fills and drops
// every later alert on that channel with no recovery; it hangs the settings
// page's test button, which calls deliverAll straight from the gin handler;
// and it leaks a goroutine per scheduled report.
//
// Longer than sendTimeout because a mail submission is several round trips
// where an HTTP post is one. A var rather than a const so the test can shrink
// it -- verifying this needs a server that never answers, and waiting out the
// real budget for that is not a test anyone will keep running.
var smtpTimeout = 30 * time.Second

// sendSMTP delivers one event by email.
//
// net/smtp rather than a mail library: this sends one short plain-text message
// with no attachments, which is the one case the standard library covers
// completely.
func sendSMTP(cfg Config, e Event) error {
	body := Render(e, cfg.Lang)
	// The first line makes a better subject than a fixed string -- most mail
	// clients show it in the list, so the alert is readable without opening it.
	subject := Host() + ": " + firstLine(body)

	client, err := dialSMTP(cfg.SMTP)
	if err != nil {
		return err
	}
	defer client.Close()

	if cfg.SMTP.User != "" {
		// PlainAuth refuses to hand credentials to an unencrypted connection,
		// so "none" plus a username fails here by design rather than leaking
		// the password onto the wire.
		auth := smtp.PlainAuth("", cfg.SMTP.User, cfg.SMTP.Pass, cfg.SMTP.Host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(cfg.SMTP.From); err != nil {
		return err
	}
	for _, to := range cfg.SMTP.To {
		if err := client.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(buildMessage(cfg.SMTP, subject, body))); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func dialSMTP(cfg SMTPConfig) (*smtp.Client, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: smtpTimeout}
	// One deadline for the whole exchange rather than one per command: the
	// budget belongs to the message, and a server that answers each command
	// just slowly enough is as unreachable as one that answers none.
	deadline := time.Now().Add(smtpTimeout)

	switch strings.ToLower(cfg.Security) {
	case "tls":
		// Implicit TLS: the whole session is wrapped, typically on port 465.
		// DialWithDialer applies the dialer's timeout to the connect and the
		// handshake together; the deadline then covers the commands.
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return nil, err
		}
		if err := conn.SetDeadline(deadline); err != nil {
			conn.Close()
			return nil, err
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return client, nil

	case "starttls":
		client, err := dialPlain(dialer, addr, cfg.Host, deadline)
		if err != nil {
			return nil, err
		}
		if ok, _ := client.Extension("STARTTLS"); !ok {
			client.Close()
			// Failing here rather than continuing in the clear: the operator
			// asked for encryption, and quietly downgrading would send the
			// password and the alerts in plaintext.
			return nil, common.NewError("smtp: server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			client.Close()
			return nil, err
		}
		return client, nil

	default:
		return dialPlain(dialer, addr, cfg.Host, deadline)
	}
}

// dialPlain is smtp.Dial with the deadline armed before the greeting is read.
//
// Set on the raw socket rather than on the smtp.Client, so it survives
// StartTLS: that wraps the same connection in place, and a tls.Conn reads and
// writes through to the socket the deadline lives on.
func dialPlain(dialer *net.Dialer, addr, host string, deadline time.Time) (*smtp.Client, error) {
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return client, nil
}

// buildMessage assembles the RFC 5322 message. Headers are CRLF-separated and
// the subject is RFC 2047 encoded, without which a non-English alert arrives as
// mojibake in most clients.
func buildMessage(cfg SMTPConfig, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + cfg.From + "\r\n")
	b.WriteString("To: " + strings.Join(cfg.To, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
