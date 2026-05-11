package mapping

import "fmt"

// fixtureCisco3850TwoMemberStack returns an ObjectIDValueMap shaped like
// a 2-member Cisco 3850 stack:
//   - sysName "3850-stack.example"
//   - entPhysical row index 1 (Switch 1, FCW2147L0K3, WS-C3850-48P)
//   - entPhysical row index 1000 (Switch 2, FCW2147L0K4, WS-C3850-48P)
//   - both entPhysicalClass=3, entPhysicalContainedIn=0
//   - entPhysicalParentRelPos populated (1, 2)
//
// Used by extractInventory + TranslateAsStack tests.
func fixtureCisco3850TwoMemberStack() ObjectIDValueMap {
	return ObjectIDValueMap{
		".1.3.6.1.2.1.1.5.0":              {Value: "3850-stack.example"},
		".1.3.6.1.2.1.1.2.0":              {Value: ".1.3.6.1.4.1.9.1.2134"},
		// Member 1 (index 1).
		".1.3.6.1.2.1.47.1.1.1.1.4.1":     {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":     {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":     {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.7.1":     {Value: "Switch 1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":    {Value: "FCW2147L0K3"},
		".1.3.6.1.2.1.47.1.1.1.1.13.1":    {Value: "WS-C3850-48P"},
		// Member 2 (index 1000).
		".1.3.6.1.2.1.47.1.1.1.1.4.1000":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1000":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1000":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.7.1000":  {Value: "Switch 2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1000": {Value: "FCW2147L0K4"},
		".1.3.6.1.2.1.47.1.1.1.1.13.1000": {Value: "WS-C3850-48P"},
	}
}

// fixtureArubaCX2MemberVSF returns an ObjectIDValueMap shaped like a
// 2-member Aruba CX VSF stack:
//   - sysName "aruba-cx-stack"
//   - entPhysical rows at indices 1 and 2
//   - both entPhysicalClass=3, entPhysicalContainedIn=0
//   - parentRelPos = 1 and 2 (numeric VSF member IDs)
//   - model "Aruba-6300M-48G" on both members
func fixtureArubaCX2MemberVSF() ObjectIDValueMap {
	return ObjectIDValueMap{
		".1.3.6.1.2.1.1.5.0":           {Value: "aruba-cx-stack"},
		".1.3.6.1.2.1.47.1.1.1.1.4.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":  {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": {Value: "SG12345"},
		".1.3.6.1.2.1.47.1.1.1.1.13.1": {Value: "Aruba-6300M-48G"},
		".1.3.6.1.2.1.47.1.1.1.1.4.2":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.2":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.2":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.2": {Value: "SG12346"},
		".1.3.6.1.2.1.47.1.1.1.1.13.2": {Value: "Aruba-6300M-48G"},
	}
}

// fixtureJunosQFX4MemberVC builds a Junos QFX5100 4-member VC scenario:
//   - sysName "vc-edge-01"
//   - 4 entPhysical chassis rows at indices 1..4
//   - parentRelPos = 0 on all rows (Junos doesn't populate it)
//   - entPhysicalName = "FPC 0" .. "FPC 3" -> ids 0..3
//   - 4 distinct serials, model "EX4300-48T"
func fixtureJunosQFX4MemberVC() ObjectIDValueMap {
	out := ObjectIDValueMap{
		".1.3.6.1.2.1.1.5.0": {Value: "vc-edge-01"},
	}
	for _, member := range []struct {
		idx, fpc int
		serial   string
	}{
		{1, 0, "BR0000000001"},
		{2, 1, "BR0000000002"},
		{3, 2, "BR0000000003"},
		{4, 3, "BR0000000004"},
	} {
		prefix := func(col int) string {
			return fmt.Sprintf(".1.3.6.1.2.1.47.1.1.1.1.%d.%d", col, member.idx)
		}
		out[prefix(4)] = Value{Value: "0"}
		out[prefix(5)] = Value{Value: "3"}
		out[prefix(6)] = Value{Value: "0"} // parentRelPos unused on Junos
		out[prefix(7)] = Value{Value: fmt.Sprintf("FPC %d", member.fpc)}
		out[prefix(11)] = Value{Value: member.serial}
		out[prefix(13)] = Value{Value: "EX4300-48T"}
	}
	return out
}
