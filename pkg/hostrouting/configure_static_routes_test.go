package hostrouting

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Azure/AKSFlexNode/pkg/config"
)

func TestRenderStaticRoutesScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		routes       []config.StaticRoute
		wantContains []string
		wantErr      bool
	}{
		{
			name:   "empty is no-op",
			routes: nil,
			wantContains: []string{
				"#!/bin/bash",
				"No routes configured",
				"exit 0",
			},
		},
		{
			name: "single route with explicit gateway",
			routes: []config.StaticRoute{
				{Destination: "172.16.1.0/24", Gateway: "172.18.1.1", Dev: "eth0"},
			},
			wantContains: []string{
				`172.16.1.0/24|eth0|172.18.1.1|0`,
				`ip -4 route replace "$DEST" via "$GW" dev "$DEV"`,
			},
		},
		{
			name: "auto-resolve dev and gateway when empty",
			routes: []config.StaticRoute{
				{Destination: "172.16.2.0/24"},
			},
			wantContains: []string{
				`resolve_default_dev_cached`,
				`awk '/^default / {for (i=1;i<=NF;i++) if ($i=="dev") {print $(i+1); exit}}'`,
				`172.16.2.0/24|@@AUTO_DEV@@|@@AUTO_GW@@|0`,
				`cannot install route $DEST`,
				`ip -4 route replace "$DEST" via "$GW" dev "$DEV"`,
			},
		},
		{
			name: "metric included when nonzero",
			routes: []config.StaticRoute{
				{Destination: "10.0.0.0/8", Gateway: "10.1.0.1", Metric: 100},
			},
			wantContains: []string{
				`10.0.0.0/8|@@AUTO_DEV@@|10.1.0.1|100`,
				`metric "$METRIC"`,
			},
		},
		{
			name: "rejects invalid destination",
			routes: []config.StaticRoute{
				{Destination: "not-a-cidr"},
			},
			wantErr: true,
		},
		{
			name: "rejects invalid gateway",
			routes: []config.StaticRoute{
				{Destination: "172.16.1.0/24", Gateway: "not-an-ip"},
			},
			wantErr: true,
		},
		{
			name: "rejects IPv6 destination",
			routes: []config.StaticRoute{
				{Destination: "2001:db8::/32"},
			},
			wantErr: true,
		},
		{
			name: "rejects IPv6 gateway",
			routes: []config.StaticRoute{
				{Destination: "172.16.1.0/24", Gateway: "2001:db8::1"},
			},
			wantErr: true,
		},
		{
			name: "auto-resolve fails hard when gateway never appears",
			routes: []config.StaticRoute{
				{Destination: "172.16.2.0/24"},
			},
			wantContains: []string{
				"resolve_default_gw",
				"|| { echo",
				"exit 1",
			},
		},
		{
			name: "rejects shell-meta in dev name",
			routes: []config.StaticRoute{
				{Destination: "172.16.1.0/24", Dev: "eth0; rm -rf /"},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := renderStaticRoutesScript(tc.routes)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got script:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("script missing %q\nscript:\n%s", want, got)
				}
			}
		})
	}
}

func TestValidateStaticRoutesConfig(t *testing.T) {
	t.Parallel()

	routes := []config.StaticRoute{
		{Destination: "172.16.1.0/24", Gateway: "172.18.1.1"},
	}

	tests := []struct {
		name    string
		cfg     config.StaticRoutesConfig
		wantErr bool
	}{
		{
			name:    "routes without explicit opt-in are rejected",
			cfg:     config.StaticRoutesConfig{Routes: routes},
			wantErr: true,
		},
		{
			name: "routes with explicit opt-in are accepted",
			cfg:  config.StaticRoutesConfig{Enabled: true, Routes: routes},
		},
		{
			name: "no routes with opt-in disabled is allowed",
			cfg:  config.StaticRoutesConfig{Enabled: false},
		},
		{
			name: "no routes with opt-in enabled is allowed",
			cfg:  config.StaticRoutesConfig{Enabled: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStaticRoutesConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestRenderStaticRoutesScriptIsDeterministic(t *testing.T) {
	t.Parallel()
	routes := []config.StaticRoute{
		{Destination: "172.16.1.0/24", Gateway: "172.18.1.1"},
		{Destination: "172.16.2.0/24"},
	}
	first, err := renderStaticRoutesScript(routes)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := renderStaticRoutesScript(routes)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if first != second {
		t.Errorf("non-deterministic output.\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestIsSafeIfaceName(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"eth0":              true,
		"ib6":               true,
		"bond0.100":         true,
		"veth-foo_bar":      true,
		"":                  false,
		"toolong123456789a": false,
		"eth0; rm -rf /":    false,
		"$(whoami)":         false,
		"eth0 eth1":         false,
	}
	for in, want := range tests {
		if got := isSafeIfaceName(in); got != want {
			t.Errorf("isSafeIfaceName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWriteScriptIfChanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	// First write (file does not exist).
	changed, err := writeScriptIfChanged(path, []byte("hello"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !changed {
		t.Errorf("first write: expected changed=true")
	}

	// Identical content — should not touch the file.
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	changed, err = writeScriptIfChanged(path, []byte("hello"))
	if err != nil {
		t.Fatalf("noop write: %v", err)
	}
	if changed {
		t.Errorf("noop write: expected changed=false")
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("noop write touched the file (mtime changed)")
	}

	// Different content — should update and report changed.
	changed, err = writeScriptIfChanged(path, []byte("goodbye"))
	if err != nil {
		t.Fatalf("update write: %v", err)
	}
	if !changed {
		t.Errorf("update write: expected changed=true")
	}
	got, err := os.ReadFile(path) //#nosec G304 -- path is t.TempDir()-scoped
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "goodbye" {
		t.Errorf("file content = %q, want %q", got, "goodbye")
	}
}

func TestCleanupSystemdUnit(t *testing.T) {
	t.Parallel()

	newLogger := func() *slog.Logger {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	t.Run("skips when no files exist", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		var calls []string
		run := func(context.Context, *slog.Logger, ...string) error {
			calls = append(calls, "unexpected")
			return nil
		}

		err := cleanupSystemdUnit(
			context.Background(),
			newLogger(),
			"static-routes.service",
			filepath.Join(root, "static-routes.service"),
			filepath.Join(root, "static-routes.sh"),
			run,
		)
		if err != nil {
			t.Fatalf("cleanupSystemdUnit: %v", err)
		}
		if len(calls) != 0 {
			t.Fatalf("systemctl calls = %v, want none", calls)
		}
	})

	t.Run("removes stale unit and script", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		unitPath := filepath.Join(root, "static-routes.service")
		scriptPath := filepath.Join(root, "static-routes.sh")
		if err := os.WriteFile(unitPath, []byte("unit"), 0o644); err != nil {
			t.Fatalf("write unit: %v", err)
		}
		if err := os.WriteFile(scriptPath, []byte("script"), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		var calls []string
		run := func(_ context.Context, _ *slog.Logger, args ...string) error {
			calls = append(calls, strings.Join(args, " "))
			return nil
		}

		err := cleanupSystemdUnit(
			context.Background(),
			newLogger(),
			"static-routes.service",
			unitPath,
			scriptPath,
			run,
		)
		if err != nil {
			t.Fatalf("cleanupSystemdUnit: %v", err)
		}
		for _, path := range []string{unitPath, scriptPath} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s still exists or stat failed with unexpected error: %v", path, err)
			}
		}
		want := []string{
			"stop static-routes.service",
			"disable static-routes.service",
			"daemon-reload",
		}
		if strings.Join(calls, "\n") != strings.Join(want, "\n") {
			t.Fatalf("systemctl calls = %v, want %v", calls, want)
		}
	})
}
