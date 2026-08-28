package service

import (
	"sort"
	"strings"
	"testing"
)

// notifySettings seeds its map from defaultValueMap and ignores DB rows for
// keys it does not find there, so a setting the code reads but never defaulted
// reads as its zero value forever -- and SettingService.Save's UPDATE touches
// no row, so the settings page cannot store it either. Both failures are
// silent.
//
// notifyBotEnable shipped exactly that way and was only caught by deploying:
// the bot could not be switched on, and the toggle did not persist.
//
// This pins the full list. Adding a setting means adding it here too, which is
// the point -- the diff is where someone notices the default is missing.
func TestNotifyDefaultsAreComplete(t *testing.T) {
	want := []string{
		"notifyBackup",
		"notifyBotEnable",
		"notifyCpu",
		"notifyEnable",
		"notifyEvents",
		"notifyExpireDays",
		"notifyLang",
		"notifyMemory",
		"notifyNodeFlap",
		"notifyOutboundUrl",
		"notifyProxy",
		"notifyReport",
		"notifySmtpFrom",
		"notifySmtpHost",
		"notifySmtpPass",
		"notifySmtpPort",
		"notifySmtpSecurity",
		"notifySmtpTo",
		"notifySmtpUser",
		"notifyTgApiServer",
		"notifyTgChatId",
		"notifyTgToken",
		"notifyVolumeGB",
		"notifyWebhookUrl",
	}

	var got []string
	for key := range defaultValueMap {
		if strings.HasPrefix(key, "notify") {
			got = append(got, key)
		}
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Errorf("defaultValueMap has %d notify keys, expected %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	inGot := make(map[string]bool, len(got))
	for _, k := range got {
		inGot[k] = true
	}
	for _, k := range want {
		if !inGot[k] {
			t.Errorf("%q is read by the notify code but has no default -- it will never be settable", k)
		}
	}
	inWant := make(map[string]bool, len(want))
	for _, k := range want {
		inWant[k] = true
	}
	for _, k := range got {
		if !inWant[k] {
			t.Errorf("%q was added to defaultValueMap without being listed here", k)
		}
	}
}
