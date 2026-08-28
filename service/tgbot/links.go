package tgbot

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/sub"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	qrcode "github.com/skip2/go-qrcode"
)

// qrSize is the width of the generated PNG in pixels. Large enough that a phone
// scans it off another phone's screen without the reader hunting for the
// corners, small enough that the upload is a few kilobytes.
const qrSize = 512

// linkListLimit bounds how many links one message offers. A client on twenty
// inbounds is unusual; a keyboard with twenty rows is unusable.
const linkListLimit = 15

// clientLinks resolves one client's share links with their remarks kept.
//
// clientInfo is deliberately empty. That argument appends the remaining traffic
// and days to a link's label, which is fine inside a subscription body the
// client's app parses, but this hands the link to a person and prints it into a
// QR code -- neither should carry the account's quota. The subscription page
// withholds it for the same reason.
func clientLinks(name string) ([]sub.Link, error) {
	c, err := findClient(name)
	if err != nil {
		return nil, err
	}
	var linkService sub.LinkService
	return linkService.GetLinkList(&c.Links, "all", ""), nil
}

// linkLabel is what a link's button says. Remarks are set per inbound and are
// normally present; a link without one still needs something to press.
func linkLabel(link sub.Link, index int) string {
	if remark := strings.TrimSpace(link.Remark); remark != "" {
		return remark
	}
	return t("links.unnamed", p("n", strconv.Itoa(index+1)))
}

// sendClientLinks offers one button per share link.
//
// self decides whose buttons these are: an end user's carry only an index and
// are resolved against the client bound to their chat, so nothing they can
// press names an account. An operator's carry the name, because they are
// looking at somebody else's card by definition.
func sendClientLinks(ctx context.Context, b *bot.Bot, chatID int64, name string, self bool) {
	links, err := clientLinks(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), backMarkup(name, self))
		return
	}
	if len(links) == 0 {
		reply(ctx, b, chatID, t("links.none", nil), backMarkup(name, self))
		return
	}

	var rows [][]models.InlineKeyboardButton
	for i, link := range links {
		if i == linkListLimit {
			break
		}
		data := payloadPrefix + payloads.put("link|"+strconv.Itoa(i)+"|"+name)
		if self {
			data = payloadPrefix + payloads.put("selflink|"+strconv.Itoa(i))
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: linkLabel(link, i), CallbackData: data}})
	}

	text := t("links.title", p("count", strconv.Itoa(len(links))))
	if len(links) > linkListLimit {
		text += "\n" + t("links.more", p("count", strconv.Itoa(len(links)-linkListLimit)))
	}
	rows = append(rows, backRow(name, self))
	reply(ctx, b, chatID, text, &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

// sendClientLink sends one link as text and again as a QR code.
//
// Both, not one: the text is what gets pasted into a client on the same device,
// and the QR is what gets scanned from another one. A URI too long or too odd
// to encode still gets sent as text rather than failing the whole reply.
func sendClientLink(ctx context.Context, b *bot.Bot, chatID int64, name string, index int, self bool) {
	links, err := clientLinks(name)
	if err != nil {
		reply(ctx, b, chatID, err.Error(), backMarkup(name, self))
		return
	}
	if index < 0 || index >= len(links) {
		// The list is rebuilt on every press, so a button from before an edit
		// can point past the end.
		reply(ctx, b, chatID, t("expired", nil), backMarkup(name, self))
		return
	}

	link := links[index]
	label := linkLabel(link, index)
	reply(ctx, b, chatID, label+"\n"+link.Uri, nil)

	png, err := qrcode.Encode(link.Uri, qrcode.Medium, qrSize)
	if err != nil {
		logger.Warning("tgbot: encoding a QR code for ", name, " failed: ", err)
		reply(ctx, b, chatID, t("links.noQr", nil), backMarkup(name, self))
		return
	}
	_, err = b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:      chatID,
		Photo:       &models.InputFileUpload{Filename: "qr.png", Data: bytes.NewReader(png)},
		Caption:     label,
		ReplyMarkup: backMarkup(name, self),
	})
	if err != nil {
		logger.Warning("tgbot: sending a QR code failed: ", err)
	}
}

// backRow is the way back out of a link listing, which differs by audience: an
// end user returns to their own usage, an operator to the client's card.
func backRow(name string, self bool) []models.InlineKeyboardButton {
	if self {
		return []models.InlineKeyboardButton{
			{Text: t("btn.back", nil), CallbackData: staticPrefix + "self"},
		}
	}
	return []models.InlineKeyboardButton{
		{Text: t("btn.back", nil), CallbackData: payloadPrefix + payloads.put("client|"+name)},
	}
}

func backMarkup(name string, self bool) models.ReplyMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{backRow(name, self)}}
}
