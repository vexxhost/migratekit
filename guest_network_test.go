package main

import (
	"strings"
	"testing"
)

func TestGuestNetworkValidation(t *testing.T) {
	for _, value := range []string{
		"00:11:22:33:44:55:ip:192.0.2.10",
		"AA:BB:CC:DD:EE:FF:ip:192.0.2.10",
		"00:11:22:33:44:55:ip:192.0.2.10,,24",
		"00:11:22:33:44:55:ip:192.0.2.10,192.0.2.1,24,192.0.2.53,2001:db8::53",
		"00:11:22:33:44:55:ip:2001:db8::10,2001:db8::1,64,2001:db8::53",
	} {
		if err := validateGuestNetworkOptions([]string{value}, true); err != nil {
			t.Errorf("valid input rejected: %s: %v", value, err)
		}
	}
	for _, value := range []string{
		"invalid", "00:11:22:33:44:55:bridge:public",
		"00-11-22-33-44-55:ip:192.0.2.10",
		"0011.2233.4455:ip:192.0.2.10",
		"00:11:22:33:44:55:66:77:ip:192.0.2.10",
		"00:11:22:33:44:55:ip:",
		"00:11:22:33:44:55:ip:192.0.2.999",
		"00:11:22:33:44:55:ip:192.0.2.10,invalid,24",
		"00:11:22:33:44:55:ip:192.0.2.10,2001:db8::1,24",
		"00:11:22:33:44:55:ip:192.0.2.10,,33",
		"00:11:22:33:44:55:ip:2001:db8::10,,129",
		"00:11:22:33:44:55:ip:192.0.2.10,,-1",
		"00:11:22:33:44:55:ip:192.0.2.10,,wrong",
		"00:11:22:33:44:55:ip:192.0.2.10,,24,invalid",
	} {
		if err := validateGuestNetworkOptions([]string{value}, true); err == nil {
			t.Errorf("invalid input accepted: %s", value)
		}
	}
}

func TestGuestNetworkErrorsPrecedeVMwareConnection(t *testing.T) {
	previousConfigs, previousV2V := guestNetworkConfigs, enablev2v
	t.Cleanup(func() { guestNetworkConfigs, enablev2v = previousConfigs, previousV2V })
	for _, tc := range []struct {
		value   string
		convert bool
	}{
		{"invalid", true},
		{"00:11:22:33:44:55:ip:192.0.2.10,,24", false},
	} {
		guestNetworkConfigs = []string{tc.value}
		enablev2v = tc.convert
		err := rootCmd.PersistentPreRunE(cutoverCmd, nil)
		if err == nil || !strings.Contains(err.Error(), "--guest-network-config") {
			t.Fatalf("expected network option error before VMware connection: %v", err)
		}
	}
}
