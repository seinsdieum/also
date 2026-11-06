package cli

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Tests must not call the GitHub release API.
	os.Setenv("ALSO_NO_UPDATE_CHECK", "1")
	os.Exit(m.Run())
}
