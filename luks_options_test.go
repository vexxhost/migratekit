package main

import (
	"strings"
	"testing"
)

func TestLUKSKeysRequireConversionBeforeAuthentication(t *testing.T) {
	for _, test := range []struct {
		command string
		convert bool
	}{{"migrate", true}, {"cutover", false}} {
		_, err := validateLUKSKeyOptions([]string{"all=/missing-key"}, test.convert, test.command)
		if err == nil || !strings.Contains(err.Error(), "requires cutover") {
			t.Fatalf("expected option validation before opening key or authenticating: %v", err)
		}
	}
}
