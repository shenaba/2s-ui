package cmd

import (
	"strings"
	"testing"
)

// The two resets this guards are irreversible and reachable by a typo, so
// anything that is not a plain yes has to be a no -- including the case with no
// terminal to ask on, which is what a systemd unit or a provisioning script
// looks like from here.
func TestConfirmAcceptsOnlyAPlainYes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"y", "y\n", true},
		{"yes", "yes\n", true},
		{"uppercase", "YES\n", true},
		{"padded", "  y  \n", true},
		{"no", "n\n", false},
		{"enter", "\n", false},
		{"anything else", "sure\n", false},
		{"the word in a sentence", "yes please\n", false},
		// No terminal: the read returns EOF straight away.
		{"eof", "", false},
		// A terminal that closed after the answer, with no newline.
		{"yes without a newline", "y", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmFrom(strings.NewReader(tt.input), ""); got != tt.want {
				t.Errorf("confirmFrom(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
