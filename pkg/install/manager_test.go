package install

import (
	"testing"

	"k8s-offline-tool/pkg/config"
)

func TestIsPrimaryExecutionNode(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		node config.NodeConfig
		want bool
	}{
		{
			name: "non-HA master deploys addons",
			cfg: config.Config{
				InstallMode: config.InstallModeFull,
				HA:          config.HAConfig{Enabled: false},
			},
			node: config.NodeConfig{IsMaster: true},
			want: true,
		},
		{
			name: "HA primary master deploys addons",
			cfg: config.Config{
				InstallMode: config.InstallModeAddonsOnly,
				HA:          config.HAConfig{Enabled: true},
			},
			node: config.NodeConfig{IsMaster: true, IsPrimaryMaster: true},
			want: true,
		},
		{
			name: "HA secondary master should skip addons",
			cfg: config.Config{
				InstallMode: config.InstallModeFull,
				HA:          config.HAConfig{Enabled: true},
			},
			node: config.NodeConfig{IsMaster: true, IsPrimaryMaster: false},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &Manager{
				globalCfg: &tt.cfg,
				nodeCfg:   &tt.node,
			}
			got := mgr.isPrimaryExecutionNode()
			if got != tt.want {
				t.Fatalf("isPrimaryExecutionNode()=%v, want %v", got, tt.want)
			}
		})
	}
}
