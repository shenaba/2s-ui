package tgbot

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// onFormInput advances the new-client conversation by one step.
//
// Each step validates before storing, so a rejected answer leaves the form on
// the same question rather than carrying a bad value forward to fail at the
// end -- by which point the operator has answered three more questions for
// nothing.
func onFormInput(ctx context.Context, b *bot.Bot, chatID int64, form formState, text string) {
	draft := form.Draft

	switch form.Step {
	case stepClientName:
		name := strings.TrimSpace(text)
		if name == "" {
			reply(ctx, b, chatID, "The name cannot be empty. Send a name.", nil)
			return
		}
		// Checked here as well as in ClientService.Save. Save would reject it
		// too, but only after the operator had answered every other question.
		if _, err := findClient(name); err == nil {
			reply(ctx, b, chatID, "A client named "+name+" already exists. Send a different name.", nil)
			return
		}
		draft.Name = name
		forms.set(chatID, stepClientVolume, draft)
		reply(ctx, b, chatID, "Traffic limit in GB. Send 0 for unlimited.", nil)

	case stepClientVolume:
		gb, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || gb < 0 {
			reply(ctx, b, chatID, "Send a whole number of GB, or 0 for unlimited.", nil)
			return
		}
		draft.VolumeGB = gb
		forms.set(chatID, stepClientExpiry, draft)
		reply(ctx, b, chatID, "Days until expiry. Send 0 for no expiry.", nil)

	case stepClientExpiry:
		days, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || days < 0 {
			reply(ctx, b, chatID, "Send a whole number of days, or 0 for no expiry.", nil)
			return
		}
		draft.ExpiryDays = days
		forms.set(chatID, stepClientExpiry, draft)
		reply(ctx, b, chatID, draftSummary(draft),
			confirmKeyboard(payloadPrefix+payloads.put("client.create!|")))
	}
}

func draftSummary(d clientDraft) string {
	var b strings.Builder
	b.WriteString("Create this client?\n")
	b.WriteString("Name: " + d.Name)
	if d.VolumeGB > 0 {
		b.WriteString("\nTraffic: " + strconv.FormatInt(d.VolumeGB, 10) + " GB")
	} else {
		b.WriteString("\nTraffic: unlimited")
	}
	if d.ExpiryDays > 0 {
		b.WriteString("\nExpires in: " + strconv.Itoa(d.ExpiryDays) + " day(s)")
	} else {
		b.WriteString("\nExpires: never")
	}
	// Said out loud because it is a decision the form did not ask about: a
	// client attached to nothing is inert, and picking inbounds over chat would
	// be several more questions for what is almost always the same answer.
	b.WriteString("\nInbounds: all local inbounds")
	return b.String()
}

// createClient commits the draft.
func createClient(ctx context.Context, b *bot.Bot, chatID int64) {
	form, ok := forms.get(chatID)
	if !ok || form.Draft.Name == "" {
		reply(ctx, b, chatID, "That form has expired. Start again from the menu.", mainMenu())
		return
	}
	d := form.Draft
	forms.clear(chatID)

	config, err := service.NewClientConfig(d.Name)
	if err != nil {
		reply(ctx, b, chatID, "Could not generate credentials: "+err.Error(), mainMenu())
		return
	}
	inbounds, err := service.LocalInboundIDs()
	if err != nil {
		reply(ctx, b, chatID, "Could not read the inbound list: "+err.Error(), mainMenu())
		return
	}
	inboundsJSON, err := json.Marshal(inbounds)
	if err != nil {
		reply(ctx, b, chatID, "Could not encode the inbound list: "+err.Error(), mainMenu())
		return
	}

	client := model.Client{
		Enable:   true,
		Name:     d.Name,
		Config:   config,
		Inbounds: inboundsJSON,
		Links:    json.RawMessage("[]"),
		Volume:   d.VolumeGB << 30,
	}
	if d.ExpiryDays > 0 {
		client.Expiry = time.Now().AddDate(0, 0, d.ExpiryDays).Unix()
	}

	data, err := json.Marshal(client)
	if err != nil {
		reply(ctx, b, chatID, "Could not encode the client: "+err.Error(), mainMenu())
		return
	}
	if err := save(chatID, "clients", "new", data); err != nil {
		reply(ctx, b, chatID, "Save failed: "+err.Error(), mainMenu())
		return
	}

	created, err := findClient(d.Name)
	if err != nil {
		// Saved but not readable back: report the success rather than an error,
		// since the client does exist.
		reply(ctx, b, chatID, "Created "+d.Name+".", mainMenu())
		return
	}
	markup := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "Open", CallbackData: payloadPrefix + payloads.put("client|"+d.Name)},
		{Text: "Menu", CallbackData: staticPrefix + "status"},
	}}}
	reply(ctx, b, chatID, "Created.\n"+clientDetail(*created), markup)
}
