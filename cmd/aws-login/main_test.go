package main

import (
	"testing"

	"github.com/qiushiyan/clikit/clikittest"
)

// Every help page, pinned; make golden accepts an intended change.
func TestHelpPages(t *testing.T) {
	clikittest.Golden(t, app, "testdata/help")
}
