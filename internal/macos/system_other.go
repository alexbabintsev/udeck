//go:build !darwin

package macos

func ReadCPUTicks() (CPUTicks, error)    { return CPUTicks{}, ErrUnsupported }
func NetCounters() ([]NetCounter, error) { return nil, ErrUnsupported }
func Disk(string) (DiskSpace, error)     { return DiskSpace{}, ErrUnsupported }
func Battery() (BatteryState, error)     { return BatteryState{}, ErrUnsupported }
