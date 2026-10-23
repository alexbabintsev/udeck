//go:build darwin

package widgets

/*
#include <mach/mach.h>
#include <sys/sysctl.h>

// host is looked up once: every mach_host_self call adds a reference to
// the port that is never released.
static mach_port_t host;

static int read_vm_stats(int64_t *total, int64_t *page_size,
		int64_t *free_pages, int64_t *inactive, int64_t *speculative, int64_t *purgeable) {
	uint64_t memsize = 0;
	size_t len = sizeof(memsize);
	if (sysctlbyname("hw.memsize", &memsize, &len, NULL, 0) != 0) {
		return -1;
	}
	if (host == MACH_PORT_NULL) {
		host = mach_host_self();
	}
	vm_size_t ps = 0;
	if (host_page_size(host, &ps) != KERN_SUCCESS) {
		return -2;
	}
	vm_statistics64_data_t vm;
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	kern_return_t kr = host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &count);
	if (kr != KERN_SUCCESS) {
		return kr;
	}
	*total = (int64_t)memsize;
	*page_size = (int64_t)ps;
	// vm_stat reports free pages without the speculative ones.
	*free_pages = (int64_t)vm.free_count - (int64_t)vm.speculative_count;
	*inactive = (int64_t)vm.inactive_count;
	*speculative = (int64_t)vm.speculative_count;
	*purgeable = (int64_t)vm.purgeable_count;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"sync"
)

var vmStatsMu sync.Mutex // guards the lazily set host port

func readVMStats() (vmStats, error) {
	var total, pageSize, free, inactive, speculative, purgeable C.int64_t
	vmStatsMu.Lock()
	rc := C.read_vm_stats(&total, &pageSize, &free, &inactive, &speculative, &purgeable)
	vmStatsMu.Unlock()
	if rc != 0 {
		return vmStats{}, fmt.Errorf("read memory statistics: code %d", int(rc))
	}
	return vmStats{
		total:       int64(total),
		pageSize:    int64(pageSize),
		free:        int64(free),
		inactive:    int64(inactive),
		speculative: int64(speculative),
		purgeable:   int64(purgeable),
	}, nil
}
