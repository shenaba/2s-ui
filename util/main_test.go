package util

import (
	"os"
	"testing"

	"github.com/shenaba/2s-ui/logger"

	"github.com/op/go-logging"
)

// TestMain initialises the s-ui logger before any test runs. Link generation
// reports a malformed out_json and an unparseable generated link through it,
// and an uninitialised logger panics.
func TestMain(m *testing.M) {
	logger.InitLogger(logging.ERROR)
	os.Exit(m.Run())
}
