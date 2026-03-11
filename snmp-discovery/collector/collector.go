package collector

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/data"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/metrics"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/profiles"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/snmp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	sysDescrOID    = "1.3.6.1.2.1.1.1"
	sysObjectIDOID = "1.3.6.1.2.1.1.2"
	sysNameOID     = "1.3.6.1.2.1.1.5"
)

// MetricsCollector collects SNMP operational metrics from devices using ktranslate profiles
// and exports them via the configured OTLP endpoint.
type MetricsCollector struct {
	clientFactory snmp.ClientFactory
	matcher       *profiles.Matcher
	deviceLookup  data.DeviceRetriever
	logger        *slog.Logger
	snmpTimeout   time.Duration
	retries       int
}

// NewMetricsCollector creates a MetricsCollector.
func NewMetricsCollector(clientFactory snmp.ClientFactory, matcher *profiles.Matcher, deviceLookup data.DeviceRetriever, logger *slog.Logger, snmpTimeout time.Duration, retries int) *MetricsCollector {
	return &MetricsCollector{
		clientFactory: clientFactory,
		matcher:       matcher,
		deviceLookup:  deviceLookup,
		logger:        logger,
		snmpTimeout:   snmpTimeout,
		retries:       retries,
	}
}

// CollectTarget collects SNMP metrics from a single target using its matched profile.
// Returns nil if the device has no matching profile (not an error condition).
func (c *MetricsCollector) CollectTarget(ctx context.Context, target config.Target, auth *config.Authentication, policyName string) error {
	walker, err := c.clientFactory(target.Host, target.Port, c.retries, c.snmpTimeout, auth, c.logger)
	if err != nil {
		return fmt.Errorf("creating SNMP client for %s: %w", target.Host, err)
	}
	defer func() {
		if err := walker.Close(); err != nil {
			c.logger.Warn("Error closing SNMP connection", "host", target.Host, "error", err)
		}
	}()

	if err := walker.Connect(); err != nil {
		return fmt.Errorf("connecting to %s: %w", target.Host, err)
	}

	// Fetch sysObjectID for profile matching.
	sysOIDValue, err := c.walkScalar(walker, sysObjectIDOID)
	if err != nil {
		return fmt.Errorf("getting sysObjectID from %s: %w", target.Host, err)
	}

	// Fetch sysDescr for matches-redirect resolution (best-effort).
	sysDescr, _ := c.walkScalar(walker, sysDescrOID)

	profile, ok := c.matcher.MatchWithDescr(sysOIDValue, sysDescr)
	if !ok {
		c.logger.Debug("No SNMP profile matched", "host", target.Host, "sysObjectID", sysOIDValue)
		return nil
	}
	c.logger.Debug("Matched SNMP profile", "host", target.Host, "sysObjectID", sysOIDValue, "profile", profile.FileName)

	deviceName := target.Host
	if name, err := c.walkScalar(walker, sysNameOID); err == nil && name != "" {
		deviceName = name
	}

	baseAttrs := []attribute.KeyValue{
		attribute.String("device_ip", target.Host),
		attribute.String("device_name", deviceName),
		attribute.String("profile_name", profileName(profile)),
		attribute.String("policy", policyName),
	}
	if c.deviceLookup != nil {
		if deviceType, err := c.deviceLookup.GetDevice(sysOIDValue); err == nil && deviceType != "" {
			baseAttrs = append(baseAttrs, attribute.String("device_type", deviceType))
		}
	}

	// Collect top-level metric_tags as additional device-level attributes.
	deviceTagAttrs := c.collectDeviceTags(walker, profile.MetricTags)
	baseAttrs = append(baseAttrs, deviceTagAttrs...)

	for _, entry := range profile.Metrics {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Symbol != nil {
			c.collectScalar(ctx, walker, entry.Symbol, baseAttrs)
		} else if entry.Table != nil {
			c.collectTable(ctx, walker, &entry, baseAttrs)
		}
	}
	return nil
}

// collectDeviceTags walks the top-level profile metric_tags and returns them as OTLP attributes.
// These are device-wide scalar OIDs (e.g. sysName, sysLocation) inherited from system-mib.yml.
func (c *MetricsCollector) collectDeviceTags(walker snmp.Walker, metricTags []profiles.MetricTag) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	for _, mt := range metricTags {
		col := metricTagColumn(&mt)
		if col == nil || col.OID == "" {
			continue
		}
		val, err := c.walkScalar(walker, col.OID)
		if err != nil || val == "" {
			continue
		}
		tagName := mt.Tag
		if tagName == "" {
			tagName = col.Name
		}
		if tagName == "" {
			continue
		}
		attrs = append(attrs, attribute.String(strings.ToLower(tagName), val))
	}
	return attrs
}

// collectScalar collects a single scalar OID metric.
func (c *MetricsCollector) collectScalar(ctx context.Context, walker snmp.Walker, sym *profiles.Symbol, baseAttrs []attribute.KeyValue) {
	pdus, err := walker.Walk(sym.OID, 0)
	if err != nil {
		c.logger.Debug("Error walking scalar OID", "oid", sym.OID, "name", sym.Name, "error", err)
		return
	}
	for _, pdu := range pdus {
		val, strVal, err := pduToValue(pdu, sym.Conversion)
		if err != nil {
			c.logger.Debug("Skipping non-numeric PDU", "oid", sym.OID, "name", sym.Name, "error", err)
			continue
		}

		attrs := make([]attribute.KeyValue, len(baseAttrs))
		copy(attrs, baseAttrs)
		if len(sym.Enum) > 0 {
			if name := enumName(sym.Enum, val); name != "" {
				attrs = append(attrs, attribute.String(sym.Name+"_status", name))
			}
		}
		if sym.Tag != "" {
			attrs = append(attrs, attribute.String("tag", sym.Tag))
		}
		if strVal != "" {
			attrs = append(attrs, attribute.String(sym.Name+"_value", strVal))
		}

		g := metrics.GetGauge(buildMetricName(sym.Name), sym.Name+" (SNMP profile metric)")
		if g != nil {
			g.Record(ctx, val, metric.WithAttributes(attrs...))
		}
	}
}

// conditionCheck holds the parsed condition for a table symbol.
type conditionCheck struct {
	columnOID string
	expected  int64
}

// collectTable collects all columns in an SNMP table, joining metric and tag columns by row index.
// Symbols with a condition field are only emitted for rows where the condition is satisfied.
func (c *MetricsCollector) collectTable(ctx context.Context, walker snmp.Walker, entry *profiles.MetricEntry, baseAttrs []attribute.KeyValue) {
	// --- Tag columns ---
	// rowTags: rowIndex -> tag name -> tag value string
	rowTags := make(map[string]map[string]string)
	for _, mt := range entry.MetricTags {
		col := metricTagColumn(&mt)
		if col == nil || col.OID == "" {
			continue
		}
		pdus, err := walker.Walk(col.OID, 1)
		if err != nil {
			c.logger.Debug("Error walking tag column", "oid", col.OID, "tag", mt.Tag, "error", err)
			continue
		}
		for fullOID, pdu := range pdus {
			rowIdx := extractRowIndex(fullOID, col.OID)
			if rowTags[rowIdx] == nil {
				rowTags[rowIdx] = make(map[string]string)
			}
			tagName := mt.Tag
			if tagName == "" {
				tagName = col.Name
			}
			rowTags[rowIdx][tagName] = pduToString(pdu, col)
		}
	}

	// --- Conditions ---
	// Build a name->OID index for all symbols in this table entry.
	symOIDByName := make(map[string]string, len(entry.Symbols))
	for _, sym := range entry.Symbols {
		symOIDByName[sym.Name] = sym.OID
	}
	// Parse conditions and walk their referenced column OIDs.
	// conditionRowVals: conditionColumnOID -> rowIdx -> int64 value
	conditionRowVals := make(map[string]map[string]int64)
	// conditions: symbol OID -> conditionCheck (only for symbols that have conditions)
	conditions := make(map[string]conditionCheck)
	for _, sym := range entry.Symbols {
		if sym.Condition == "" {
			continue
		}
		parts := strings.SplitN(sym.Condition, "=", 2)
		if len(parts) != 2 {
			c.logger.Warn("Ignoring malformed condition", "symbol", sym.Name, "condition", sym.Condition)
			continue
		}
		refName := strings.TrimSpace(parts[0])
		expectedStr := strings.TrimSpace(parts[1])
		expected, err := strconv.ParseInt(expectedStr, 10, 64)
		if err != nil {
			c.logger.Warn("Ignoring condition with non-integer value", "symbol", sym.Name, "condition", sym.Condition)
			continue
		}
		refOID, ok := symOIDByName[refName]
		if !ok {
			c.logger.Warn("Condition references unknown symbol", "symbol", sym.Name, "ref", refName)
			continue
		}
		conditions[sym.OID] = conditionCheck{columnOID: refOID, expected: expected}
		// Walk the condition column if not already walked.
		if _, walked := conditionRowVals[refOID]; !walked {
			pdus, err := walker.Walk(refOID, 1)
			if err != nil {
				c.logger.Debug("Error walking condition column", "oid", refOID, "error", err)
				conditionRowVals[refOID] = nil
				continue
			}
			rowVals := make(map[string]int64, len(pdus))
			for fullOID, pdu := range pdus {
				rowIdx := extractRowIndex(fullOID, refOID)
				if v, _, err := pduToValue(pdu, ""); err == nil {
					rowVals[rowIdx] = v
				}
			}
			conditionRowVals[refOID] = rowVals
		}
	}

	// --- Metric columns ---
	for i := range entry.Symbols {
		sym := &entry.Symbols[i]
		pdus, err := walker.Walk(sym.OID, 1)
		if err != nil {
			c.logger.Debug("Error walking table column", "oid", sym.OID, "name", sym.Name, "error", err)
			continue
		}
		// Pre-look up condition for this symbol (zero value = no condition).
		cond, hasCondition := conditions[sym.OID]

		for fullOID, pdu := range pdus {
			rowIdx := extractRowIndex(fullOID, sym.OID)

			// Apply condition filter.
			if hasCondition {
				rowVals := conditionRowVals[cond.columnOID]
				if rowVals == nil {
					continue // condition column walk failed; skip all rows
				}
				if rowVals[rowIdx] != cond.expected {
					continue
				}
			}

			val, strVal, err := pduToValue(pdu, sym.Conversion)
			if err != nil {
				continue
			}

			rowAttrs := make([]attribute.KeyValue, len(baseAttrs))
			copy(rowAttrs, baseAttrs)
			rowAttrs = append(rowAttrs, attribute.String("row_index", rowIdx))
			if tags, ok := rowTags[rowIdx]; ok {
				for k, v := range tags {
					rowAttrs = append(rowAttrs, attribute.String(k, v))
				}
			}
			if len(sym.Enum) > 0 {
				if name := enumName(sym.Enum, val); name != "" {
					rowAttrs = append(rowAttrs, attribute.String(sym.Name+"_status", name))
				}
			}
			if sym.Tag != "" {
				rowAttrs = append(rowAttrs, attribute.String("tag", sym.Tag))
			}
			if strVal != "" {
				rowAttrs = append(rowAttrs, attribute.String(sym.Name+"_value", strVal))
			}

			g := metrics.GetGauge(buildMetricName(sym.Name), sym.Name+" (SNMP profile table metric)")
			if g != nil {
				g.Record(ctx, val, metric.WithAttributes(rowAttrs...))
			}
		}
	}
}

// walkScalar walks a scalar OID subtree and returns the first string value found.
func (c *MetricsCollector) walkScalar(walker snmp.Walker, oid string) (string, error) {
	pdus, err := walker.Walk(oid, 0)
	if err != nil {
		return "", err
	}
	for _, pdu := range pdus {
		switch pdu.Type {
		case gosnmp.OctetString:
			if s, ok := pdu.Value.(string); ok {
				return strings.TrimSpace(s), nil
			}
			if b, ok := pdu.Value.([]byte); ok {
				return strings.TrimSpace(string(b)), nil
			}
		case gosnmp.ObjectIdentifier:
			if s, ok := pdu.Value.(string); ok {
				return s, nil
			}
		case gosnmp.IPAddress:
			if s, ok := pdu.Value.(string); ok {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("no value returned for OID %s", oid)
}

// extractRowIndex strips the column OID prefix from a full OID to get the row index suffix.
// Example: fullOID="1.3.6.1.2.1.2.2.1.2.3", columnOID="1.3.6.1.2.1.2.2.1.2" -> "3"
func extractRowIndex(fullOID, columnOID string) string {
	prefix := columnOID + "."
	if strings.HasPrefix(fullOID, prefix) {
		return fullOID[len(prefix):]
	}
	return fullOID
}

// buildMetricName converts a profile symbol name to an OTLP metric name.
func buildMetricName(symbolName string) string {
	return "snmp." + strings.ToLower(symbolName)
}

// profileName returns a human-readable name for a profile (provider or filename).
func profileName(p *profiles.Profile) string {
	if p.Provider != "" {
		return p.Provider
	}
	return p.FileName
}

// metricTagColumn returns the tag column from a MetricTag, handling the
// alias where some profiles use "symbol" instead of "column".
func metricTagColumn(mt *profiles.MetricTag) *profiles.TagColumn {
	if mt.Column != nil {
		return mt.Column
	}
	return mt.Symbol
}

// pduToValue converts a PDU to an int64 metric value, applying conversion rules.
// It also returns an optional non-empty string for display-only conversions (hextoip, hwaddr, regexp).
// Returns an error for PDU types that cannot produce a numeric value.
func pduToValue(pdu snmp.PDU, conversion string) (int64, string, error) {
	// conversion: to_one — always emit 1 regardless of actual PDU type.
	if conversion == "to_one" {
		return 1, "", nil
	}

	switch pdu.Type {
	case gosnmp.Integer:
		if v, ok := pdu.Value.(int); ok {
			return int64(v), "", nil
		}
	case gosnmp.Counter32, gosnmp.Gauge32:
		if v, ok := pdu.Value.(uint); ok {
			return int64(v), "", nil //nolint:gosec
		}
	case gosnmp.Counter64:
		if v, ok := pdu.Value.(uint64); ok {
			return int64(v), "", nil //nolint:gosec
		}
	case gosnmp.TimeTicks:
		if v, ok := pdu.Value.(uint32); ok {
			return int64(v), "", nil
		}
	case gosnmp.OctetString:
		raw := pduRawBytes(pdu)
		rawStr := strings.TrimSpace(string(raw))
		switch {
		case conversion == "hextoip":
			if ip := hexBytesToIP(raw); ip != "" {
				return 1, ip, nil
			}
		case conversion == "hwaddr":
			if mac := net.HardwareAddr(raw).String(); mac != "" {
				return 1, mac, nil
			}
		case strings.HasPrefix(conversion, "hextoint:"):
			if v, err := applyHexToInt(raw, conversion); err == nil {
				return v, "", nil
			}
		case strings.HasPrefix(conversion, "regexp:"):
			if v, display, err := applyRegexp(rawStr, conversion); err == nil {
				return v, display, nil
			}
		}
		return 0, "", fmt.Errorf("non-numeric OctetString PDU (conversion=%q)", conversion)
	}
	return 0, "", fmt.Errorf("non-numeric PDU type %v", pdu.Type)
}

// applyHexToInt converts an OctetString byte slice to an integer using
// the hextoint:<endianness>:<type> conversion rule.
func applyHexToInt(raw []byte, conversion string) (int64, error) {
	// Format: hextoint:<endianness>:<type>
	// endianness: BigEndian | LittleEndian
	// type: uint16 | uint32 | uint64
	parts := strings.SplitN(conversion, ":", 3)
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid hextoint format: %s", conversion)
	}
	endianStr := parts[1]
	typeStr := parts[2]

	// If the raw bytes look like a hex string, decode them first.
	decoded := raw
	if b, err := hex.DecodeString(strings.TrimSpace(string(raw))); err == nil {
		decoded = b
	}

	var order binary.ByteOrder
	switch endianStr {
	case "BigEndian":
		order = binary.BigEndian
	case "LittleEndian":
		order = binary.LittleEndian
	default:
		return 0, fmt.Errorf("unknown endianness: %s", endianStr)
	}

	switch typeStr {
	case "uint16":
		if len(decoded) < 2 {
			return 0, fmt.Errorf("too few bytes for uint16")
		}
		return int64(order.Uint16(decoded[:2])), nil
	case "uint32":
		if len(decoded) < 4 {
			return 0, fmt.Errorf("too few bytes for uint32")
		}
		return int64(order.Uint32(decoded[:4])), nil //nolint:gosec
	case "uint64":
		if len(decoded) < 8 {
			return 0, fmt.Errorf("too few bytes for uint64")
		}
		return int64(order.Uint64(decoded[:8])), nil //nolint:gosec
	}
	return 0, fmt.Errorf("unknown hextoint type: %s", typeStr)
}

// applyRegexp applies a regexp: conversion to a string value.
// The pattern is expected to have at least one capture group; the first group is returned.
// If no capture group is present the full match is used.
// The extracted string is parsed as int64; on success the numeric value is returned.
// The original extracted string is also returned for use as a display attribute.
func applyRegexp(raw, conversion string) (int64, string, error) {
	pattern := strings.TrimPrefix(conversion, "regexp:")
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, "", fmt.Errorf("invalid regexp %q: %w", pattern, err)
	}
	matches := re.FindStringSubmatch(raw)
	if len(matches) == 0 {
		return 0, "", fmt.Errorf("regexp %q did not match %q", pattern, raw)
	}
	extracted := matches[0]
	if len(matches) > 1 {
		extracted = matches[1]
	}
	v, err := strconv.ParseInt(strings.TrimSpace(extracted), 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("regexp extracted non-integer %q: %w", extracted, err)
	}
	return v, extracted, nil
}

// pduRawBytes returns the byte slice from an OctetString PDU.
func pduRawBytes(pdu snmp.PDU) []byte {
	if b, ok := pdu.Value.([]byte); ok {
		return b
	}
	if s, ok := pdu.Value.(string); ok {
		return []byte(s)
	}
	return nil
}

// hexBytesToIP converts a raw 4-byte or 16-byte slice to an IP string.
func hexBytesToIP(raw []byte) string {
	if len(raw) == 4 || len(raw) == 16 {
		return net.IP(raw).String()
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err == nil && (len(decoded) == 4 || len(decoded) == 16) {
		return net.IP(decoded).String()
	}
	return ""
}

// pduToString converts a PDU value to a human-readable string for tag/attribute use.
// Applies enum mapping and conversion from TagColumn if present.
func pduToString(pdu snmp.PDU, col *profiles.TagColumn) string {
	switch pdu.Type {
	case gosnmp.OctetString:
		raw := pduRawBytes(pdu)
		if col != nil {
			switch col.Conversion {
			case "hextoip":
				if ip := hexBytesToIP(raw); ip != "" {
					return ip
				}
			case "hwaddr":
				if mac := net.HardwareAddr(raw).String(); mac != "" {
					return mac
				}
			}
		}
		return strings.TrimSpace(string(raw))
	case gosnmp.Integer:
		if v, ok := pdu.Value.(int); ok {
			if col != nil {
				for name, intVal := range col.Enum {
					if intVal == v {
						return name
					}
				}
			}
			return fmt.Sprintf("%d", v)
		}
	case gosnmp.Counter32, gosnmp.Gauge32:
		if v, ok := pdu.Value.(uint); ok {
			return fmt.Sprintf("%d", v)
		}
	case gosnmp.IPAddress, gosnmp.ObjectIdentifier:
		if s, ok := pdu.Value.(string); ok {
			return s
		}
	}
	return fmt.Sprintf("%v", pdu.Value)
}

// enumName returns the enum string for val, or "" if not found.
func enumName(enum map[string]int, val int64) string {
	for name, intVal := range enum {
		if int64(intVal) == val {
			return name
		}
	}
	return ""
}
