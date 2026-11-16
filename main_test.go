package main

import (
	"reflect"
	"testing"
)

func TestGuestNetworkConfigKeepsCommaSeparatedFields(t *testing.T) {
	want := []string{
		"00:11:22:33:44:55:ip:192.0.2.10,192.0.2.1,24,192.0.2.53",
		"00:11:22:33:44:66:ip:198.51.100.10,,24",
	}
	previous := guestNetworkConfigs
	t.Cleanup(func() { guestNetworkConfigs = previous })
	err := rootCmd.ParseFlags([]string{"--guest-network-config", want[0], "--guest-network-config", want[1]})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guestNetworkConfigs, want) {
		t.Fatalf("guest network configuration split or changed: %#v", guestNetworkConfigs)
	}
}
