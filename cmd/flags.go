package cmd

import (
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
)

type NetworkMapping struct {
	MACAddr   net.HardwareAddr
	NetworkID uuid.UUID
	SubnetID  uuid.UUID
	IPAddress net.IP
}

type NetworkMappingFlag struct {
	Mappings map[string]NetworkMapping
}

func (m *NetworkMappingFlag) String() string {
	var mappings []string
	for _, mapping := range m.Mappings {
		mappings = append(mappings, fmt.Sprintf("mac=%s,network-id=%s,subnet-id=%s,ip=%s", mapping.MACAddr, mapping.NetworkID, mapping.SubnetID, mapping.IPAddress))
	}
	return strings.Join(mappings, ",")
}

func (m *NetworkMappingFlag) Set(value string) error {
	mapping := NetworkMapping{}

	for _, part := range strings.Split(value, ",") {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			return fmt.Errorf("invalid network mapping: %s", value)
		}

		switch kv[0] {
		case "mac":
			mac, err := net.ParseMAC(kv[1])
			if err != nil {
				return fmt.Errorf("invalid MAC address: %s", kv[1])
			}
			mapping.MACAddr = mac
		case "network-id":
			networkID, err := uuid.Parse(kv[1])
			if err != nil {
				return fmt.Errorf("invalid network ID: %s", kv[1])
			}
			mapping.NetworkID = networkID
		case "subnet-id":
			subnetID, err := uuid.Parse(kv[1])
			if err != nil {
				return fmt.Errorf("invalid subnet ID: %s", kv[1])
			}
			mapping.SubnetID = subnetID
		case "ip":
			ip := net.ParseIP(kv[1])
			if ip == nil {
				return fmt.Errorf("invalid IP address: %s", kv[1])
			}
			mapping.IPAddress = ip
		default:
			return fmt.Errorf("unknown network mapping key: %s", kv[0])
		}
	}

	if mapping.MACAddr == nil {
		return fmt.Errorf("missing MAC address in network mapping: %s", value)
	}

	if mapping.NetworkID == uuid.Nil {
		return fmt.Errorf("missing network ID in network mapping: %s", value)
	}

	if mapping.SubnetID == uuid.Nil {
		return fmt.Errorf("missing subnet ID in network mapping: %s", value)
	}

	if m.Mappings == nil {
		m.Mappings = make(map[string]NetworkMapping)
	}

	m.Mappings[mapping.MACAddr.String()] = mapping
	return nil
}

func (m *NetworkMappingFlag) Type() string {
	return "networkMapping"
}
