package mailer

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	pbMailer "ganigeorgiev.com/pocketbase/pocketbase/tools/mailer"

	"miform/internal/parser"
)

// Mailer handles composing and delivering email notifications and autoresponders.
type Mailer struct {
	app core.App
}

// New creates a new Mailer instance with the given PocketBase App.
func New(app core.App) *Mailer {
	return &Mailer{app: app}
}

// SendNotification dispatches an HTML and plain-text notification email to the form's recipients.
func (m *Mailer) SendNotification(form *core.Record, parsed *parser.ParsedForm) error {
	recipientsStr := form.GetString("recipient_emails")
	toAddrs := ParseAddressList(recipientsStr)
	if len(toAddrs) == 0 {
		return errors.New("no valid recipient email addresses configured for this form")
	}

	// Prepare CC addresses
	var ccAddrs []mail.Address
	if ccStr := form.GetString("cc_emails"); strings.TrimSpace(ccStr) != "" {
		ccAddrs = append(ccAddrs, ParseAddressList(ccStr)...)
	}
	if parsed.Controls.CCEmail != "" {
		ccAddrs = append(ccAddrs, ParseAddressList(parsed.Controls.CCEmail)...)
	}

	// Prepare BCC addresses
	var bccAddrs []mail.Address
	if bccStr := form.GetString("bcc_emails"); strings.TrimSpace(bccStr) != "" {
		bccAddrs = append(bccAddrs, ParseAddressList(bccStr)...)
	}

	senderName := ExtractSenderName(m.app, form, parsed)
	senderAddr := ExtractSenderAddress(m.app)
	subject := ExtractSubject(form, parsed)
	replyTo := ExtractReplyTo(parsed)
	introText := ExtractIntroText(form, parsed)

	data := NotificationData{
		FormName:    form.GetString("name"),
		IntroText:   introText,
		Fields:      PrepareRenderFields(parsed.Fields),
		IP:          parsed.Metadata.IP,
		Referer:     parsed.Metadata.Referer,
		UserAgent:   parsed.Metadata.UserAgent,
		SubmittedAt: time.Now().UTC().Format("Jan 02, 2006 15:04:05 UTC"),
	}

	htmlBody, err := RenderNotificationHTML(data)
	if err != nil {
		return fmt.Errorf("failed to render html notification: %w", err)
	}

	textBody, err := RenderNotificationText(data)
	if err != nil {
		return fmt.Errorf("failed to render text notification: %w", err)
	}

	msg := &pbMailer.Message{
		From: mail.Address{
			Name:    senderName,
			Address: senderAddr,
		},
		To:      toAddrs,
		Cc:      ccAddrs,
		Bcc:     bccAddrs,
		Subject: subject,
		HTML:    htmlBody,
		Text:    textBody,
		Headers: make(map[string]string),
	}

	if replyTo != "" {
		msg.Headers["Reply-To"] = replyTo
	}

	return m.app.NewMailClient().Send(msg)
}

// SendAutoresponder sends an automated confirmation email to the submitter if enabled on the form.
func (m *Mailer) SendAutoresponder(form *core.Record, parsed *parser.ParsedForm) error {
	if !form.GetBool("autoresponder_enabled") {
		return nil
	}

	recipient := ExtractReplyTo(parsed)
	if recipient == "" {
		// Submitter did not provide an email address; cannot autorespond
		return nil
	}

	senderName := ExtractSenderName(m.app, form, parsed)
	senderAddr := ExtractSenderAddress(m.app)

	formName := form.GetString("name")
	subject := strings.TrimSpace(form.GetString("autoresponder_subject"))
	if subject == "" {
		subject = "Thank you for contacting " + formName
	}

	message := strings.TrimSpace(form.GetString("autoresponder_message"))
	if message == "" {
		message = fmt.Sprintf("Hello,\n\nThank you for reaching out to us. We have received your submission and will get back to you shortly.\n\nBest regards,\n%s", formName)
	}

	data := AutoresponderData{
		FormName: formName,
		Message:  message,
	}

	htmlBody, err := RenderAutoresponderHTML(data)
	if err != nil {
		return fmt.Errorf("failed to render autoresponder html: %w", err)
	}

	textBody, err := RenderAutoresponderText(data)
	if err != nil {
		return fmt.Errorf("failed to render autoresponder text: %w", err)
	}

	toAddrs := ParseAddressList(recipient)
	if len(toAddrs) == 0 {
		return nil
	}

	msg := &pbMailer.Message{
		From: mail.Address{
			Name:    senderName,
			Address: senderAddr,
		},
		To:      toAddrs,
		Subject: subject,
		HTML:    htmlBody,
		Text:    textBody,
		Headers: make(map[string]string),
	}

	return m.app.NewMailClient().Send(msg)
}

// ParseAddressList splits a comma-separated list of email addresses.
func ParseAddressList(raw string) []mail.Address {
	var result []mail.Address
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := mail.ParseAddress(part)
		if err == nil {
			result = append(result, *addr)
		} else {
			result = append(result, mail.Address{Address: part})
		}
	}
	return result
}

// ExtractReplyTo identifies the visitor's email address to configure the Reply-To header.
func ExtractReplyTo(parsed *parser.ParsedForm) string {
	if parsed.Controls.ReplyTo != "" {
		return strings.TrimSpace(parsed.Controls.ReplyTo)
	}
	for k, v := range parsed.Data {
		if strings.EqualFold(k, "email") || strings.EqualFold(k, "reply_to") || strings.EqualFold(k, "replyto") {
			if strVal, ok := v.(string); ok && strings.TrimSpace(strVal) != "" {
				return strings.TrimSpace(strVal)
			}
		}
	}
	return ""
}

// ExtractSubject determines the final subject line for notification emails.
func ExtractSubject(form *core.Record, parsed *parser.ParsedForm) string {
	if strings.TrimSpace(parsed.Controls.Subject) != "" {
		return strings.TrimSpace(parsed.Controls.Subject)
	}
	if form != nil {
		if sub := form.GetString("email_subject"); strings.TrimSpace(sub) != "" {
			return strings.TrimSpace(sub)
		}
		if name := form.GetString("name"); strings.TrimSpace(name) != "" {
			return "New submission: " + name
		}
	}
	return "New submission"
}

// ExtractIntroText determines the optional intro text banner.
func ExtractIntroText(form *core.Record, parsed *parser.ParsedForm) string {
	if strings.TrimSpace(parsed.Controls.IntroText) != "" {
		return strings.TrimSpace(parsed.Controls.IntroText)
	}
	if form != nil {
		return form.GetString("intro_text")
	}
	return ""
}

// ExtractSenderName determines the display name for the email From header.
func ExtractSenderName(app core.App, form *core.Record, parsed *parser.ParsedForm) string {
	if form != nil && strings.TrimSpace(form.GetString("sender_name")) != "" {
		return strings.TrimSpace(form.GetString("sender_name"))
	}
	if strings.TrimSpace(parsed.Controls.FromName) != "" {
		return strings.TrimSpace(parsed.Controls.FromName)
	}
	if app != nil && strings.TrimSpace(app.Settings().Meta.SenderName) != "" {
		return strings.TrimSpace(app.Settings().Meta.SenderName)
	}
	if app != nil && strings.TrimSpace(app.Settings().Meta.AppName) != "" {
		return strings.TrimSpace(app.Settings().Meta.AppName)
	}
	return "MiForm"
}

// ExtractSenderAddress retrieves the verified system sender address.
func ExtractSenderAddress(app core.App) string {
	if app != nil && strings.TrimSpace(app.Settings().Meta.SenderAddress) != "" {
		return strings.TrimSpace(app.Settings().Meta.SenderAddress)
	}
	return "noreply@localhost"
}
