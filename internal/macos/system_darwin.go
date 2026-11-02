//go:build darwin

package macos

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <mach/mach.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/sysctl.h>
#include <net/if.h>
#include <net/if_dl.h>
#include <net/route.h>
#include <stdlib.h>
#include <string.h>
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>

static int cpu_ticks(unsigned long long *busy, unsigned long long *total) {
	static mach_port_t host = MACH_PORT_NULL;
	if (host == MACH_PORT_NULL) host = mach_host_self();
	host_cpu_load_info_data_t info;
	mach_msg_type_number_t count = HOST_CPU_LOAD_INFO_COUNT;
	if (host_statistics(host, HOST_CPU_LOAD_INFO, (host_info_t)&info, &count) != KERN_SUCCESS) return -1;
	unsigned long long user = info.cpu_ticks[CPU_STATE_USER], sys = info.cpu_ticks[CPU_STATE_SYSTEM],
		nice = info.cpu_ticks[CPU_STATE_NICE], idle = info.cpu_ticks[CPU_STATE_IDLE];
	*busy = user + sys + nice;
	*total = user + sys + nice + idle;
	return 0;
}

#define MAX_IFACES 128

typedef struct {
	int count;
	char name[MAX_IFACES][IFNAMSIZ];
	unsigned long long rx[MAX_IFACES], tx[MAX_IFACES];
} net_list;

// net_counters reads the 64-bit byte counters of every interface
// (NET_RT_IFLIST2; the 32-bit getifaddrs counters wrap at 4 GB).
static int net_counters(net_list *out) {
	int mib[6] = {CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0};
	size_t len = 0;
	if (sysctl(mib, 6, NULL, &len, NULL, 0) != 0) return -1;
	char *buf = malloc(len);
	if (buf == NULL) return -1;
	if (sysctl(mib, 6, buf, &len, NULL, 0) != 0) { free(buf); return -1; }
	out->count = 0;
	for (char *next = buf; next < buf + len && out->count < MAX_IFACES; ) {
		struct if_msghdr *ifm = (struct if_msghdr *)next;
		if (ifm->ifm_msglen == 0) break;
		if (ifm->ifm_type == RTM_IFINFO2) {
			struct if_msghdr2 *if2 = (struct if_msghdr2 *)ifm;
			struct sockaddr_dl *sdl = (struct sockaddr_dl *)(if2 + 1);
			int n = sdl->sdl_nlen < IFNAMSIZ - 1 ? sdl->sdl_nlen : IFNAMSIZ - 1;
			memcpy(out->name[out->count], sdl->sdl_data, n);
			out->name[out->count][n] = 0;
			out->rx[out->count] = if2->ifm_data.ifi_ibytes;
			out->tx[out->count] = if2->ifm_data.ifi_obytes;
			out->count++;
		}
		next += ifm->ifm_msglen;
	}
	free(buf);
	return 0;
}

static int cf_int(CFDictionaryRef d, CFStringRef key, int *out) {
	CFNumberRef n = CFDictionaryGetValue(d, key);
	return n != NULL && CFNumberGetValue(n, kCFNumberIntType, out);
}

// battery reads the internal battery; returns 0 when there is none.
static int battery(int *percent, int *charging, int *on_ac) {
	int found = 0;
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (info == NULL) return 0;
	CFArrayRef list = IOPSCopyPowerSourcesList(info);
	if (list == NULL) { CFRelease(info); return 0; }
	for (CFIndex i = 0; i < CFArrayGetCount(list) && !found; i++) {
		CFDictionaryRef d = IOPSGetPowerSourceDescription(info, CFArrayGetValueAtIndex(list, i));
		if (d == NULL) continue;
		CFStringRef type = CFDictionaryGetValue(d, CFSTR(kIOPSTypeKey));
		if (type == NULL || !CFEqual(type, CFSTR(kIOPSInternalBatteryType))) continue;
		int cur = 0, max = 0;
		if (!cf_int(d, CFSTR(kIOPSCurrentCapacityKey), &cur) || !cf_int(d, CFSTR(kIOPSMaxCapacityKey), &max) || max <= 0) continue;
		*percent = (int)((cur * 100.0) / max + 0.5);
		CFBooleanRef ch = CFDictionaryGetValue(d, CFSTR(kIOPSIsChargingKey));
		*charging = ch != NULL && CFBooleanGetValue(ch);
		CFStringRef state = CFDictionaryGetValue(d, CFSTR(kIOPSPowerSourceStateKey));
		*on_ac = state != NULL && CFEqual(state, CFSTR(kIOPSACPowerValue));
		found = 1;
	}
	CFRelease(list);
	CFRelease(info);
	return found;
}
*/
import "C"

import (
	"errors"
	"syscall"
	"unsafe"
)

// ReadCPUTicks returns the cumulative CPU counters of all cores.
func ReadCPUTicks() (CPUTicks, error) {
	var busy, total C.ulonglong
	if C.cpu_ticks(&busy, &total) != 0 {
		return CPUTicks{}, errors.New("host_statistics failed")
	}
	return CPUTicks{Busy: uint64(busy), Total: uint64(total)}, nil
}

// NetCounters returns the byte counters of every network interface.
func NetCounters() ([]NetCounter, error) {
	list := (*C.net_list)(C.malloc(C.sizeof_net_list))
	defer C.free(unsafe.Pointer(list))
	if C.net_counters(list) != 0 {
		return nil, errors.New("sysctl NET_RT_IFLIST2 failed")
	}
	out := make([]NetCounter, 0, int(list.count))
	for i := 0; i < int(list.count); i++ {
		out = append(out, NetCounter{
			Name:   C.GoString(&list.name[i][0]),
			RxByte: uint64(list.rx[i]),
			TxByte: uint64(list.tx[i]),
		})
	}
	return out, nil
}

// Disk returns the size and the space available to users of the volume
// holding path.
func Disk(path string) (DiskSpace, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskSpace{}, err
	}
	bs := uint64(st.Bsize)
	return DiskSpace{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}

// Battery returns the internal battery's state.
func Battery() (BatteryState, error) {
	var pct, charging, onAC C.int
	if C.battery(&pct, &charging, &onAC) == 0 {
		return BatteryState{}, nil
	}
	return BatteryState{Present: true, Percent: int(pct), Charging: charging != 0, OnAC: onAC != 0}, nil
}
