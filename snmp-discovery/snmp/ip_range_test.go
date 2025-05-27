package snmp

import (
	"net"
	"testing"
)

func TestParseIPRange(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		start   string
		end     string
	}{
		{
			name:    "valid CIDR",
			input:   "10.0.0.0/24",
			wantErr: false,
			start:   "10.0.0.0",
			end:     "10.0.0.255",
		},
		{
			name:    "valid range",
			input:   "10.0.0.0-100",
			wantErr: false,
			start:   "10.0.0.0",
			end:     "10.0.0.100",
		},
		{
			name:    "single IP",
			input:   "10.0.0.1",
			wantErr: false,
			start:   "10.0.0.1",
			end:     "10.0.0.1",
		},
		{
			name:    "invalid CIDR",
			input:   "10.0.0.0/33",
			wantErr: true,
		},
		{
			name:    "invalid range format",
			input:   "10.0.0.0-100-200",
			wantErr: true,
		},
		{
			name:    "invalid range end",
			input:   "10.0.0.0-300",
			wantErr: true,
		},
		{
			name:    "invalid IP",
			input:   "256.0.0.0",
			wantErr: true,
		},
		{
			name:    "range start greater than end",
			input:   "10.0.0.100-50",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIPRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIPRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Start.String() != tt.start {
					t.Errorf("ParseIPRange() start = %v, want %v", got.Start, tt.start)
				}
				if got.End.String() != tt.end {
					t.Errorf("ParseIPRange() end = %v, want %v", got.End, tt.end)
				}
			}
		})
	}
}

func TestExpandRange(t *testing.T) {
	tests := []struct {
		name     string
		start    string
		end      string
		expected int
	}{
		{
			name:     "single IP",
			start:    "10.0.0.1",
			end:      "10.0.0.1",
			expected: 1,
		},
		{
			name:     "small range",
			start:    "10.0.0.1",
			end:      "10.0.0.5",
			expected: 5,
		},
		{
			name:     "CIDR /24",
			start:    "10.0.0.0",
			end:      "10.0.0.255",
			expected: 256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &IPRange{
				Start: net.ParseIP(tt.start),
				End:   net.ParseIP(tt.end),
			}
			got := r.ExpandRange()
			if len(got) != tt.expected {
				t.Errorf("ExpandRange() returned %d IPs, want %d", len(got), tt.expected)
			}
		})
	}
}

func TestExpandTargetRanges(t *testing.T) {
	tests := []struct {
		name     string
		targets  []string
		wantErr  bool
		expected int
	}{
		{
			name:     "mixed targets",
			targets:  []string{"10.0.0.1", "10.0.0.0/30", "10.0.0.10-12"},
			wantErr:  false,
			expected: 8, // 1 + 4 + 3
		},
		{
			name:     "invalid target",
			targets:  []string{"10.0.0.1", "invalid"},
			wantErr:  true,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTargetRanges(tt.targets)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandTargetRanges() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != tt.expected {
				t.Errorf("ExpandTargetRanges() returned %d IPs, want %d", len(got), tt.expected)
			}
		})
	}
}
