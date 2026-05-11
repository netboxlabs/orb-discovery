package mapping

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
