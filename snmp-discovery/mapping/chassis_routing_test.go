package mapping

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouteIfIndex_AliasTable_DirectChassis(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	// Port directly on chassis row 1 (entPhysicalIndex=1).
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.10101"}
	// Port on a card under chassis row 1000 (entPhysicalIndex=1050 -> contained=1000).
	oids[".1.3.6.1.2.1.47.1.1.1.1.4.1050"] = Value{Value: "1000"}
	oids[".1.3.6.1.2.1.47.1.1.1.1.5.1050"] = Value{Value: "9"} // module
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1050.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.10201"}

	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)

	id, ok := r.routeIfIndex(10101)
	assert.True(t, ok)
	assert.Equal(t, 1, id)

	id, ok = r.routeIfIndex(10201)
	assert.True(t, ok)
	assert.Equal(t, 2, id)
}

func TestRouteIfIndex_AliasMissing_ReturnsFalse(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack() // no alias entries
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(10101)
	assert.False(t, ok)
}

func TestRouteIfIndex_AliasValueWithoutLeadingDot(t *testing.T) {
	// Some gosnmp ObjectIdentifier renderings omit the leading dot.
	// Routing must still succeed.
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: "1.3.6.1.2.1.2.2.1.1.10101"} // no leading dot
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	id, ok := r.routeIfIndex(10101)
	assert.True(t, ok)
	assert.Equal(t, 1, id)
}

func TestRouteIfIndex_NonIfIndexAliasValueIgnored(t *testing.T) {
	// Non-ifIndex VariablePointer values (e.g. pointing at ifAlias)
	// must be skipped without crashing.
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: ".1.3.6.1.2.1.31.1.1.1.18.10101"} // ifAlias
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(10101)
	assert.False(t, ok, "non-ifIndex alias values are not usable for routing")
}

func TestRouteIfIndex_ChainTerminatesAtRoot_ReturnsFalse(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	// entPhysicalIndex 999 contained directly in root (0) — not a chassis.
	oids[".1.3.6.1.2.1.47.1.1.1.1.4.999"] = Value{Value: "0"}
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.999.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.42"}

	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(42)
	assert.False(t, ok)
}
