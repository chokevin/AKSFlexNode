package cni

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"

	"github.com/Azure/AKSFlexNode/pkg/utils/utilio"
	"github.com/Azure/unbounded/pkg/agent/phases"
)

const (
	DefaultConfigDir = "/etc/cni/net.d"

	// bridgeConfFile is the filename for the explicit local/E2E bridge CNI config.
	bridgeConfFile = "99-bridge.conf"
)

//go:embed assets/99-bridge.conf
var defaultBridgeCNIConfig []byte

type writeBridgeConfigTask struct {
	machineDir string
}

// WriteBridgeConfig returns a task that writes the bridge CNI config into the
// nspawn rootfs at /etc/cni/net.d/99-bridge.conf. Production Azure CNI nodes
// must not use this host-local fallback.
func WriteBridgeConfig(machineDir string) phases.Task {
	return &writeBridgeConfigTask{machineDir: machineDir}
}

func (t *writeBridgeConfigTask) Name() string { return "write-bridge-cni-config" }

func (t *writeBridgeConfigTask) Do(_ context.Context) error {
	confPath := filepath.Join(t.machineDir, DefaultConfigDir, bridgeConfFile)
	if err := utilio.WriteFile(confPath, defaultBridgeCNIConfig, 0o644); err != nil { //nolint:gosec // CNI config must be world-readable
		return fmt.Errorf("write CNI bridge config: %w", err)
	}
	return nil
}
