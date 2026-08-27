package notify

import (
	"crypto/tls"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/util/common"
)

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

	switch strings.ToLower(cfg.Security) {
	case "tls":
		// Implicit TLS: the whole session is wrapped, typically on port 465.
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return nil, err
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return client, nil

	case "starttls":
		client, err := smtp.Dial(addr)
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
		return smtp.Dial(addr)
	}
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
