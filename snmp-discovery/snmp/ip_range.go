package snmp

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// IPRange represents a range of IP addresses
type IPRange struct {
	Start net.IP
	End   net.IP
}

// ParseIPRange parses a string into an IPRange
// Supports both CIDR notation (e.g., "10.0.0.0/24") and range notation (e.g., "10.0.0.0-100")
func ParseIPRange(input string) (*IPRange, error) {
	// Try parsing as CIDR first
	if strings.Contains(input, "/") {
		return parseCIDR(input)
	}
	// Try parsing as range
	if strings.Contains(input, "-") {
		return parseRange(input)
	}
	// Try parsing as single IP
	ip := net.ParseIP(input)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP format: %s", input)
	}
	return &IPRange{Start: ip, End: ip}, nil
}

// parseCIDR parses a CIDR notation string (e.g., "10.0.0.0/24")
func parseCIDR(input string) (*IPRange, error) {
	_, ipnet, err := net.ParseCIDR(input)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR format: %s", err)
	}

	// Get the network address
	start := ipnet.IP.To4()
	if start == nil {
		return nil, fmt.Errorf("only IPv4 addresses are supported")
	}

	// Calculate the broadcast address (end of range)
	end := make(net.IP, len(start))
	copy(end, start)
	for i := 0; i < len(end); i++ {
		end[i] |= ^ipnet.Mask[i]
	}

	return &IPRange{Start: start, End: end}, nil
}

// parseRange parses a range notation string (e.g., "10.0.0.0-100")
func parseRange(input string) (*IPRange, error) {
	parts := strings.Split(input, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format: %s", input)
	}

	// Parse the base IP
	baseIP := net.ParseIP(strings.TrimSpace(parts[0]))
	if baseIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", parts[0])
	}
	baseIP = baseIP.To4()
	if baseIP == nil {
		return nil, fmt.Errorf("only IPv4 addresses are supported")
	}

	// Parse the range end
	endNum, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid range end: %s", parts[1])
	}
	if endNum < 0 || endNum > 255 {
		return nil, fmt.Errorf("range end must be between 0 and 255")
	}

	// Create the end IP
	endIP := make(net.IP, len(baseIP))
	copy(endIP, baseIP)
	endIP[3] = byte(endNum)

	// Ensure start is less than end
	if bytes.Compare(baseIP, endIP) > 0 {
		return nil, fmt.Errorf("range start must be less than range end")
	}

	return &IPRange{Start: baseIP, End: endIP}, nil
}

// ExpandRange converts an IPRange into a slice of individual IP addresses
func (r *IPRange) ExpandRange() []net.IP {
	var ips []net.IP
	current := make(net.IP, len(r.Start))
	copy(current, r.Start)

	for bytes.Compare(current, r.End) <= 0 {
		ip := make(net.IP, len(current))
		copy(ip, current)
		ips = append(ips, ip)

		// Increment IP
		for i := len(current) - 1; i >= 0; i-- {
			current[i]++
			if current[i] != 0 {
				break
			}
		}
	}

	return ips
}

// ExpandTargetRanges takes a list of targets (individual IPs and ranges) and returns
// a list of individual IP addresses
func ExpandTargetRanges(targets []string) ([]net.IP, error) {
	var expandedIPs []net.IP
	for _, target := range targets {
		ipRange, err := ParseIPRange(target)
		if err != nil {
			return nil, fmt.Errorf("failed to parse target %s: %w", target, err)
		}
		expandedIPs = append(expandedIPs, ipRange.ExpandRange()...)
	}
	return expandedIPs, nil
}
