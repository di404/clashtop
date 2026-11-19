package api

import (
	"path/filepath"
	"strings"
)

// Host is the domain if known, else the destination IP.
func (c *Connection) Host() string {
	if c.Metadata.Host != "" {
		return c.Metadata.Host
	}
	if c.Metadata.SniffHost != "" {
		return c.Metadata.SniffHost
	}
	return c.Metadata.DestinationIP
}

func (c *Connection) Process() string {
	if c.Metadata.Process != "" {
		return c.Metadata.Process
	}
	if c.Metadata.ProcessPath != "" {
		return filepath.Base(c.Metadata.ProcessPath)
	}
	return "-"
}

// Node is the final outbound (chains are ordered node → … → group).
func (c *Connection) Node() string {
	if len(c.Chains) == 0 {
		return "-"
	}
	return c.Chains[0]
}

func (c *Connection) RuleText() string {
	if c.RulePayload != "" {
		return c.Rule + "(" + c.RulePayload + ")"
	}
	return c.Rule
}

func (c *Connection) Direct() bool {
	for _, ch := range c.Chains {
		if ch == "DIRECT" {
			return true
		}
	}
	return false
}

var stunPorts = map[string]bool{"3478": true, "3479": true, "19302": true, "19305": true, "19306": true}

// STUN reports UDP STUN connections, whose byte counters mihomo is known to
// report wildly wrong (often 100 GB+). They are listed but never counted.
func (c *Connection) STUN() bool {
	h := strings.ToLower(c.Host())
	if strings.HasPrefix(h, "stun") || strings.Contains(h, "stun.") {
		return true
	}
	return strings.EqualFold(c.Metadata.Network, "udp") && stunPorts[c.Metadata.DestinationPort]
}
