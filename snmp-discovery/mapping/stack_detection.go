package mapping

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

const (
	entPhysicalContainedInOID   = "1.3.6.1.2.1.47.1.1.1.1.4"
	entPhysicalClassOID         = "1.3.6.1.2.1.47.1.1.1.1.5"
	entPhysicalParentRelPosOID  = "1.3.6.1.2.1.47.1.1.1.1.6"
	entPhysicalNameOID          = "1.3.6.1.2.1.47.1.1.1.1.7"
	entPhysicalSerialNumOID     = "1.3.6.1.2.1.47.1.1.1.1.11"
	entPhysicalMfgNameOID       = "1.3.6.1.2.1.47.1.1.1.1.12"
	entPhysicalModelNameOID     = "1.3.6.1.2.1.47.1.1.1.1.13"
	entAliasLogicalIndexOrZero  = "1.3.6.1.2.1.47.1.3.2.1.2"
	entPhysicalClassChassis     = 3
	entPhysicalClassContainer   = 5
	entPhysicalClassModule      = 9
	entPhysicalClassStack       = 11
	entPhysicalClassUnknown     = 0
	entPhysicalPositionUnknown  = 0
	entPhysicalContainedUnknown = -1
)

// StackDetectionOIDs returns additional OIDs required for stack detection.
func StackDetectionOIDs() map[string]int {
	return map[string]int{
		"." + entPhysicalContainedInOID:  1,
		"." + entPhysicalClassOID:        1,
		"." + entPhysicalParentRelPosOID: 1,
		"." + entPhysicalNameOID:         1,
		"." + entPhysicalSerialNumOID:    1,
		"." + entPhysicalMfgNameOID:      1,
		"." + entPhysicalModelNameOID:    1,
		"." + entAliasLogicalIndexOrZero: 1,
	}
}

type entPhysicalEntry struct {
	Index        string
	IndexInt     int
	Class        int
	ContainedIn  int
	ParentRelPos int
	Name         string
	SerialNum    string
	ModelName    string
	MfgName      string
}

type stackInfo struct {
	Active            bool
	VirtualChassis    *diode.VirtualChassis
	MemberDevices     map[string]*diode.Device
	MasterDevice      *diode.Device
	InterfaceToMember map[string]string
}

type moduleInfo struct {
	Active      bool
	ModuleBays  map[string]*diode.ModuleBay
	Modules     map[string]*diode.Module
	ModuleTypes map[string]*diode.ModuleType
}

type stackDetector struct {
	logger            *slog.Logger
	entries           map[string]*entPhysicalEntry
	interfaceToMember map[string]string
}

func NewStackDetector(logger *slog.Logger) *stackDetector {
	return &stackDetector{
		logger:            logger,
		entries:           make(map[string]*entPhysicalEntry),
		interfaceToMember: make(map[string]string),
	}
}

func (d *stackDetector) Filter(objectIDs ObjectIDValueMap) ObjectIDValueMap {
	filtered := make(ObjectIDValueMap, len(objectIDs))
	for oid, value := range objectIDs {
		if d.consume(oid, value) {
			continue
		}
		filtered[oid] = value
	}
	return filtered
}

func (d *stackDetector) consume(oid string, value Value) bool {
	normalized := strings.TrimPrefix(oid, ".")

	switch {
	case strings.HasPrefix(normalized, entPhysicalContainedInOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalContainedInOID+".")
		entry := d.getOrCreateEntry(index)
		entry.ContainedIn = parseInt(value.Value, entPhysicalContainedUnknown)
		return true
	case strings.HasPrefix(normalized, entPhysicalClassOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalClassOID+".")
		entry := d.getOrCreateEntry(index)
		entry.Class = parseInt(value.Value, entPhysicalClassUnknown)
		return true
	case strings.HasPrefix(normalized, entPhysicalParentRelPosOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalParentRelPosOID+".")
		entry := d.getOrCreateEntry(index)
		entry.ParentRelPos = parseInt(value.Value, entPhysicalPositionUnknown)
		return true
	case strings.HasPrefix(normalized, entPhysicalNameOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalNameOID+".")
		entry := d.getOrCreateEntry(index)
		entry.Name = strings.TrimSpace(value.Value)
		return true
	case strings.HasPrefix(normalized, entPhysicalSerialNumOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalSerialNumOID+".")
		entry := d.getOrCreateEntry(index)
		entry.SerialNum = strings.TrimSpace(value.Value)
		return true
	case strings.HasPrefix(normalized, entPhysicalMfgNameOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalMfgNameOID+".")
		entry := d.getOrCreateEntry(index)
		entry.MfgName = strings.TrimSpace(value.Value)
		return true
	case strings.HasPrefix(normalized, entPhysicalModelNameOID+"."):
		index := strings.TrimPrefix(normalized, entPhysicalModelNameOID+".")
		entry := d.getOrCreateEntry(index)
		entry.ModelName = strings.TrimSpace(value.Value)
		return true
	case strings.HasPrefix(normalized, entAliasLogicalIndexOrZero+"."):
		suffix := strings.TrimPrefix(normalized, entAliasLogicalIndexOrZero+".")
		parts := strings.SplitN(suffix, ".", 2)
		if len(parts) < 1 || parts[0] == "" {
			return true
		}
		entIndex := parts[0]
		ifIndex := strings.TrimSpace(value.Value)
		if ifIndex != "" && ifIndex != "0" {
			d.interfaceToMember[ifIndex] = entIndex
		}
		return true
	default:
		return false
	}
}

func (d *stackDetector) getOrCreateEntry(index string) *entPhysicalEntry {
	entry, ok := d.entries[index]
	if ok {
		return entry
	}
	entry = &entPhysicalEntry{
		Index:       index,
		IndexInt:    parseInt(index, entPhysicalPositionUnknown),
		Class:       entPhysicalClassUnknown,
		ContainedIn: entPhysicalContainedUnknown,
	}
	d.entries[index] = entry
	return entry
}

func (d *stackDetector) BuildStackInfo(registry *EntityRegistry, baseDevice *diode.Device, defaults *config.Defaults) stackInfo {
	members := d.stackMembers()
	if len(members) < 2 {
		return stackInfo{}
	}

	positions := assignPositions(members)
	stackName := stackName(baseDevice, members)
	virtualChassis := &diode.VirtualChassis{
		Name: &stackName,
	}

	memberDevices := make(map[string]*diode.Device, len(members))
	for _, member := range members {
		device := registry.GetOrCreateEntity(DeviceEntityType, ObjectIDIndex(member.Index)).(*diode.Device)
		copyDeviceDefaults(device, baseDevice, defaults)
		device.Name = stringPtrOrFallback(member.Name, defaultMemberName(stackName, positions[member.Index]))
		if member.SerialNum != "" {
			device.Serial = &member.SerialNum
		}
		if device.DeviceType == nil && member.ModelName != "" {
			manufacturer := member.MfgName
			device.DeviceType = deviceTypeFromEntityMIB(member.ModelName, manufacturer)
		}
		if device.Platform == nil && member.MfgName != "" {
			device.Platform = platformFromEntityMIB(member.MfgName)
		}
		device.VirtualChassis = virtualChassis
		position := int64(positions[member.Index])
		device.VcPosition = &position
		memberDevices[member.Index] = device
	}

	master := masterDevice(memberDevices, positions)
	if master != nil {
		virtualChassis.Master = master
	}

	return stackInfo{
		Active:            true,
		VirtualChassis:    virtualChassis,
		MemberDevices:     memberDevices,
		MasterDevice:      master,
		InterfaceToMember: d.interfaceToMember,
	}
}

func (d *stackDetector) BuildModuleInfo(baseDevice *diode.Device, memberDevices map[string]*diode.Device) moduleInfo {
	modules := d.moduleEntries()
	if len(modules) == 0 {
		return moduleInfo{}
	}

	moduleBays := make(map[string]*diode.ModuleBay)
	moduleTypes := make(map[string]*diode.ModuleType)
	moduleEntities := make(map[string]*diode.Module)

	for _, moduleEntry := range modules {
		device := d.deviceForEntry(moduleEntry, baseDevice, memberDevices)
		if device == nil {
			continue
		}

		bayEntry := d.moduleBayEntry(moduleEntry)
		bayKey := moduleBayKey(moduleEntry, bayEntry)
		bay := moduleBays[bayKey]
		if bay == nil {
			bay = buildModuleBay(device, bayEntry, moduleEntry)
			moduleBays[bayKey] = bay
		}

		moduleType := moduleTypeForEntry(moduleEntry, device, moduleTypes)
		module := &diode.Module{
			Device:     device,
			ModuleBay:  bay,
			ModuleType: moduleType,
		}
		if moduleEntry.SerialNum != "" {
			module.Serial = &moduleEntry.SerialNum
		}
		if strings.TrimSpace(moduleEntry.Name) != "" {
			description := strings.TrimSpace(moduleEntry.Name)
			module.Description = &description
		}

		moduleEntities[moduleEntry.Index] = module
	}

	if len(moduleEntities) == 0 {
		return moduleInfo{}
	}

	return moduleInfo{
		Active:      true,
		ModuleBays:  moduleBays,
		Modules:     moduleEntities,
		ModuleTypes: moduleTypes,
	}
}

func (d *stackDetector) stackMembers() []entPhysicalEntry {
	chassis := make([]entPhysicalEntry, 0)
	containers := make(map[string]entPhysicalEntry)
	for _, entry := range d.entries {
		if entry.Class == entPhysicalClassChassis {
			chassis = append(chassis, *entry)
		}
		if entry.Class == entPhysicalClassContainer || entry.Class == entPhysicalClassStack {
			containers[entry.Index] = *entry
		}
	}

	topLevelChassis := filterChassis(chassis, func(entry entPhysicalEntry) bool {
		return entry.ContainedIn == 0
	})
	if len(topLevelChassis) >= 2 {
		return topLevelChassis
	}

	for _, container := range containers {
		if container.ContainedIn != 0 {
			continue
		}
		children := filterChassis(chassis, func(entry entPhysicalEntry) bool {
			return entry.ContainedIn == container.IndexInt
		})
		if len(children) >= 2 {
			return children
		}
	}

	return nil
}

func filterChassis(entries []entPhysicalEntry, predicate func(entPhysicalEntry) bool) []entPhysicalEntry {
	filtered := make([]entPhysicalEntry, 0, len(entries))
	for _, entry := range entries {
		if predicate(entry) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func assignPositions(members []entPhysicalEntry) map[string]int {
	positions := make(map[string]int, len(members))
	used := make(map[int]bool)
	ordered := make([]entPhysicalEntry, len(members))
	copy(ordered, members)

	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		if left.ParentRelPos == right.ParentRelPos {
			return left.IndexInt < right.IndexInt
		}
		if left.ParentRelPos == 0 {
			return false
		}
		if right.ParentRelPos == 0 {
			return true
		}
		return left.ParentRelPos < right.ParentRelPos
	})

	nextPos := 1
	for _, entry := range ordered {
		pos := entry.ParentRelPos
		if pos <= 0 || used[pos] {
			for used[nextPos] {
				nextPos++
			}
			pos = nextPos
			nextPos++
		}
		positions[entry.Index] = pos
		used[pos] = true
	}

	return positions
}

func stackName(base *diode.Device, members []entPhysicalEntry) string {
	if base != nil && base.Name != nil && *base.Name != "" {
		return *base.Name
	}
	for _, member := range members {
		if member.Name != "" {
			return member.Name
		}
	}
	return "snmp-stack"
}

func defaultMemberName(stackName string, position int) string {
	if stackName == "" {
		stackName = "snmp-stack"
	}
	return fmt.Sprintf("%s-%d", stackName, position)
}

func stringPtrOrFallback(value string, fallback string) *string {
	if strings.TrimSpace(value) == "" {
		return &fallback
	}
	clean := strings.TrimSpace(value)
	return &clean
}

func masterDevice(memberDevices map[string]*diode.Device, positions map[string]int) *diode.Device {
	var master *diode.Device
	minPos := int(^uint(0) >> 1)
	for index, device := range memberDevices {
		position := positions[index]
		if position < minPos {
			minPos = position
			master = device
		}
	}
	return master
}

func copyDeviceDefaults(target *diode.Device, base *diode.Device, defaults *config.Defaults) {
	if base != nil {
		if target.DeviceType == nil {
			target.DeviceType = base.DeviceType
		}
		if target.Platform == nil {
			target.Platform = base.Platform
		}
		if target.Role == nil {
			target.Role = base.Role
		}
		if target.Site == nil {
			target.Site = base.Site
		}
		if target.Location == nil {
			target.Location = base.Location
		}
		if target.Tags == nil {
			target.Tags = base.Tags
		}
		if target.Description == nil {
			target.Description = base.Description
		}
		if target.Comments == nil {
			target.Comments = base.Comments
		}
		if target.Tenant == nil {
			target.Tenant = base.Tenant
		}
	}

	if defaults != nil {
		applyDeviceDefaults(target, defaults)
	}
}

func applyDeviceDefaults(target *diode.Device, defaults *config.Defaults) {
	if defaults.Device.Description != "" && target.Description == nil {
		target.Description = &defaults.Device.Description
	}
	if defaults.Device.Comments != "" && target.Comments == nil {
		target.Comments = &defaults.Device.Comments
	}
	if len(defaults.Device.Tags) > 0 && target.Tags == nil {
		tags := make([]*diode.Tag, 0, len(defaults.Device.Tags))
		for _, tag := range defaults.Device.Tags {
			tagName := tag
			tags = append(tags, &diode.Tag{Name: &tagName})
		}
		target.Tags = tags
	}
	if defaults.Role != "" && target.Role == nil {
		target.Role = &diode.DeviceRole{Name: &defaults.Role}
	}
	if defaults.Site != "" && target.Site == nil {
		target.Site = &diode.Site{Name: &defaults.Site}
	}
	if defaults.Location != "" && target.Location == nil {
		target.Location = &diode.Location{Name: &defaults.Location}
		if target.Location.Site == nil && defaults.Site != "" {
			target.Location.Site = &diode.Site{Name: &defaults.Site}
		}
	}
}

func deviceTypeFromEntityMIB(model string, manufacturer string) *diode.DeviceType {
	manufacturerEntity := &diode.Manufacturer{
		Name: &manufacturer,
	}
	return &diode.DeviceType{
		Model:        &model,
		Manufacturer: manufacturerEntity,
	}
}

func platformFromEntityMIB(manufacturer string) *diode.Platform {
	manufacturerEntity := &diode.Manufacturer{
		Name: &manufacturer,
	}
	return &diode.Platform{
		Name:         &manufacturer,
		Slug:         toSlug(&manufacturer),
		Manufacturer: manufacturerEntity,
	}
}

func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func (d *stackDetector) moduleEntries() []*entPhysicalEntry {
	modules := make([]*entPhysicalEntry, 0)
	for _, entry := range d.entries {
		if entry.Class == entPhysicalClassModule {
			modules = append(modules, entry)
		}
	}
	return modules
}

func (d *stackDetector) moduleBayEntry(moduleEntry *entPhysicalEntry) *entPhysicalEntry {
	if moduleEntry == nil || moduleEntry.ContainedIn <= 0 {
		return nil
	}
	parent := d.entryByIndex(moduleEntry.ContainedIn)
	if parent != nil && parent.Class == entPhysicalClassContainer {
		return parent
	}
	return nil
}

func (d *stackDetector) deviceForEntry(entry *entPhysicalEntry, baseDevice *diode.Device, memberDevices map[string]*diode.Device) *diode.Device {
	if entry == nil {
		return baseDevice
	}
	chassis := d.findAncestor(entry, func(candidate *entPhysicalEntry) bool {
		return candidate.Class == entPhysicalClassChassis
	})
	if chassis != nil && memberDevices != nil {
		if device, ok := memberDevices[chassis.Index]; ok {
			return device
		}
	}
	if baseDevice != nil {
		return baseDevice
	}
	return nil
}

func (d *stackDetector) findAncestor(entry *entPhysicalEntry, predicate func(*entPhysicalEntry) bool) *entPhysicalEntry {
	if entry == nil {
		return nil
	}
	visited := map[string]bool{entry.Index: true}
	current := entry
	for current != nil {
		if predicate(current) {
			return current
		}
		if current.ContainedIn <= 0 {
			return nil
		}
		parentIndex := strconv.Itoa(current.ContainedIn)
		if visited[parentIndex] {
			return nil
		}
		visited[parentIndex] = true
		current = d.entries[parentIndex]
	}
	return nil
}

func (d *stackDetector) entryByIndex(index int) *entPhysicalEntry {
	if index <= 0 {
		return nil
	}
	return d.entries[strconv.Itoa(index)]
}

func buildModuleBay(device *diode.Device, bayEntry *entPhysicalEntry, moduleEntry *entPhysicalEntry) *diode.ModuleBay {
	name := moduleBayName(bayEntry, moduleEntry)
	bay := &diode.ModuleBay{
		Device: device,
	}
	if name != "" {
		bay.Name = &name
	}
	position := moduleBayPosition(bayEntry, moduleEntry)
	if position != "" {
		bay.Position = &position
	}
	return bay
}

func moduleBayName(bayEntry *entPhysicalEntry, moduleEntry *entPhysicalEntry) string {
	if bayEntry != nil && strings.TrimSpace(bayEntry.Name) != "" {
		return strings.TrimSpace(bayEntry.Name)
	}
	if moduleEntry != nil && strings.TrimSpace(moduleEntry.Name) != "" {
		return strings.TrimSpace(moduleEntry.Name)
	}
	if bayEntry != nil && bayEntry.ParentRelPos > 0 {
		return fmt.Sprintf("Module Bay %d", bayEntry.ParentRelPos)
	}
	if moduleEntry != nil && moduleEntry.ParentRelPos > 0 {
		return fmt.Sprintf("Module Bay %d", moduleEntry.ParentRelPos)
	}
	if moduleEntry != nil && moduleEntry.Index != "" {
		return fmt.Sprintf("Module Bay %s", moduleEntry.Index)
	}
	return "Module Bay"
}

func moduleBayPosition(bayEntry *entPhysicalEntry, moduleEntry *entPhysicalEntry) string {
	if bayEntry != nil && bayEntry.ParentRelPos > 0 {
		return strconv.Itoa(bayEntry.ParentRelPos)
	}
	if moduleEntry != nil && moduleEntry.ParentRelPos > 0 {
		return strconv.Itoa(moduleEntry.ParentRelPos)
	}
	return ""
}

func moduleBayKey(moduleEntry *entPhysicalEntry, bayEntry *entPhysicalEntry) string {
	if bayEntry != nil && bayEntry.Index != "" {
		return "container:" + bayEntry.Index
	}
	if moduleEntry != nil && moduleEntry.Index != "" {
		return "module:" + moduleEntry.Index
	}
	return "module:unknown"
}

func moduleTypeForEntry(moduleEntry *entPhysicalEntry, device *diode.Device, cache map[string]*diode.ModuleType) *diode.ModuleType {
	if moduleEntry == nil {
		return nil
	}
	model := strings.TrimSpace(moduleEntry.ModelName)
	if model == "" {
		model = strings.TrimSpace(moduleEntry.Name)
	}
	if model == "" {
		return nil
	}
	manufacturer := strings.TrimSpace(moduleEntry.MfgName)
	if manufacturer == "" {
		manufacturer = manufacturerNameFromDevice(device)
	}
	cacheKey := moduleTypeKey(model, manufacturer)
	if cached, ok := cache[cacheKey]; ok {
		return cached
	}

	moduleType := moduleTypeFromEntityMIB(model, manufacturer)
	if moduleType != nil {
		cache[cacheKey] = moduleType
	}
	return moduleType
}

func moduleTypeKey(model string, manufacturer string) string {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	normalizedManufacturer := strings.ToLower(strings.TrimSpace(manufacturer))
	return normalizedManufacturer + "::" + normalizedModel
}

func moduleTypeFromEntityMIB(model string, manufacturer string) *diode.ModuleType {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	moduleType := &diode.ModuleType{
		Model: &model,
	}
	if strings.TrimSpace(manufacturer) != "" {
		manufacturerEntity := &diode.Manufacturer{
			Name: &manufacturer,
		}
		moduleType.Manufacturer = manufacturerEntity
	}
	return moduleType
}

func manufacturerNameFromDevice(device *diode.Device) string {
	if device == nil {
		return ""
	}
	if device.DeviceType != nil && device.DeviceType.Manufacturer != nil && device.DeviceType.Manufacturer.Name != nil {
		return strings.TrimSpace(*device.DeviceType.Manufacturer.Name)
	}
	if device.Platform != nil && device.Platform.Manufacturer != nil && device.Platform.Manufacturer.Name != nil {
		return strings.TrimSpace(*device.Platform.Manufacturer.Name)
	}
	return ""
}
