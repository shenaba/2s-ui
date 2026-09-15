package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// confirmFrom asks before something irreversible and reports whether the answer
// was yes.
//
// Anything that is not a plain yes is a no, including a read that fails. That
// is what makes it safe in a script or a unit file: with no terminal to ask on,
// the read returns immediately and the caller stops instead of going ahead
// unattended.
func confirmFrom(in io.Reader, prompt string) bool {
	fmt.Print(prompt)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		fmt.Println()
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

func confirm(prompt string) bool {
	return confirmFrom(os.Stdin, prompt)
}
