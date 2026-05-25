package infra

import (
	"os"
	"strings"
	"testing"
)

func TestFlexNodeVMModuleRequestsAzureCNIPodIPInventory(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("modules/vm.bicep")
	if err != nil {
		t.Fatalf("read VM module: %v", err)
	}
	module := string(data)

	for _, want := range []string{
		"param secondaryPrivateIPAddressCount int = 0",
		"range(0, secondaryPrivateIPAddressCount)",
		"name: 'pod-ip-${i + 1}'",
		"privateIPAllocationMethod: 'Dynamic'",
		"primary: false",
		"ipConfigurations: concat([",
	} {
		if !strings.Contains(module, want) {
			t.Fatalf("VM module missing %q", want)
		}
	}
}

func TestE2EInfraPassesPodIPInventoryToFlexNodeVMs(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("main.bicep")
	if err != nil {
		t.Fatalf("read main Bicep: %v", err)
	}
	main := string(data)

	if !strings.Contains(main, "param flexNodePodIPInventoryCount int = 32") {
		t.Fatal("main Bicep does not define the flex node pod IP inventory count")
	}
	if got := strings.Count(main, "secondaryPrivateIPAddressCount: flexNodePodIPInventoryCount"); got != 3 {
		t.Fatalf("secondary pod IP inventory passed to %d VM modules, want 3", got)
	}
}
