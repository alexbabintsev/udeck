package macos

// CPUTicks are the cumulative CPU time counters of all cores; usage is
// the busy share of the difference between two readings.
type CPUTicks struct {
	Busy, Total uint64
}

// Usage is the busy share of the time between prev and t, 0..1.
func (t CPUTicks) Usage(prev CPUTicks) float64 {
	total := t.Total - prev.Total
	if t.Total < prev.Total || t.Busy < prev.Busy || total == 0 {
		return 0
	}
	return min(float64(t.Busy-prev.Busy)/float64(total), 1)
}

// NetCounter is the cumulative traffic of one network interface.
type NetCounter struct {
	Name   string
	RxByte uint64
	TxByte uint64
}

// DiskSpace is the size of a volume and the space available to users.
type DiskSpace struct {
	Total, Free uint64
}

// Used is the share of the volume not available, 0..1.
func (d DiskSpace) Used() float64 {
	if d.Total == 0 || d.Free > d.Total {
		return 0
	}
	return float64(d.Total-d.Free) / float64(d.Total)
}

// BatteryState is the internal battery; Present is false on desktops.
type BatteryState struct {
	Present  bool
	Percent  int
	Charging bool
	OnAC     bool
}
