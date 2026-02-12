package config

import (
	"testing"
)

func TestApplyDefaultsAndValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "Empty nodes",
			cfg: &Config{
				Nodes: []NodeConfig{},
			},
			wantErr: true,
		},
		{
			name: "Valid basic config",
			cfg: &Config{
				Nodes: []NodeConfig{
					{IP: "192.168.1.1", Password: "pass", IsMaster: true},
				},
				InstallMode: InstallModeFull,
			},
			wantErr: false,
		},
		{
			name: "Missing node IP",
			cfg: &Config{
				Nodes: []NodeConfig{
					{IP: "", Password: "pass", IsMaster: true},
				},
				InstallMode: InstallModeFull,
			},
			wantErr: true,
		},
		{
			name: "Invalid HA config (less than 3 masters)",
			cfg: &Config{
				Nodes: []NodeConfig{
					{IP: "192.168.1.1", Password: "pass", IsMaster: true, IsPrimaryMaster: true, Interface: "eth0"},
				},
				InstallMode: InstallModeFull,
				HA:          HAConfig{Enabled: true, VirtualIP: "192.168.1.100"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ApplyDefaultsAndValidate(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDefaultsAndValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
