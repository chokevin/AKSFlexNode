package daemon

import (
	"testing"

	"github.com/Azure/AKSFlexNode/pkg/config"
	"github.com/Azure/unbounded/pkg/agent/phases"
)

func TestRootfsPrepTasksCNI(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg        *config.Config
		wantBridge bool
	}{
		"default config uses Azure CNI contract": {
			cfg:        &config.Config{},
			wantBridge: false,
		},
		"explicit Azure CNI does not install bridge fallback": {
			cfg:        &config.Config{CNI: config.CNIConfig{Mode: config.CNIModeAzure}},
			wantBridge: false,
		},
		"explicit bridge mode installs bridge fallback": {
			cfg:        &config.Config{CNI: config.CNIConfig{Mode: config.CNIModeBridge}},
			wantBridge: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tasks := rootfsPrepTasks(tt.cfg, t.TempDir())
			gotBridge := hasTask(tasks, "write-bridge-cni-config")
			if gotBridge != tt.wantBridge {
				t.Fatalf("bridge fallback task present = %t, want %t", gotBridge, tt.wantBridge)
			}
		})
	}
}

func hasTask(tasks []phases.Task, name string) bool {
	for _, task := range tasks {
		if task.Name() == name {
			return true
		}
	}
	return false
}
