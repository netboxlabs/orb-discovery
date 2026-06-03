package policy

import "github.com/netboxlabs/diode-sdk-go/diode"

// annotateEntitiesWithRunID stamps run_id on each entity's Diode metadata and on
// the nested Device shared by Interface/Module. gNMI emits only Device,
// Interface, Module, and ModuleBay, all sharing the same *Device pointer, so a
// shallow walk covers the batch. Existing metadata keys (e.g. source_match) are
// preserved — only run_id is set.
func annotateEntitiesWithRunID(entities []diode.Entity, runID string) {
	set := func(md *diode.Metadata) {
		if *md == nil {
			*md = diode.Metadata{}
		}
		(*md)["run_id"] = runID
	}
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Device:
			if v != nil {
				set(&v.Metadata)
			}
		case *diode.Interface:
			if v != nil {
				set(&v.Metadata)
				if v.Device != nil {
					set(&v.Device.Metadata)
				}
			}
		case *diode.Module:
			if v != nil {
				set(&v.Metadata)
				if v.Device != nil {
					set(&v.Device.Metadata)
				}
			}
		case *diode.ModuleBay:
			if v != nil {
				set(&v.Metadata)
			}
		}
	}
}
