package config

import (
	"errors"
	"fmt"
	"strings"
)

func stringInSlice(str string, slice []string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

// ApplyDefaultsAndValidate applies default values and validates the configuration
func ApplyDefaultsAndValidate(cfg *Config) error {
	if cfg.ResourcePackage == "" {
		return errors.New("Error: resource_package is required in config.yaml")
	}
	if len(cfg.Nodes) == 0 {
		return errors.New("Error: No nodes defined in config.yaml")
	}
	if cfg.CommandTimeoutSeconds <= 0 {
		cfg.CommandTimeoutSeconds = 600
	}
	if !stringInSlice(cfg.InstallMode, SupportedInstallModes) {
		return fmt.Errorf("Error: install_mode %s is not supported.", cfg.InstallMode)
	}

	versions := []struct {
		name      string
		value     *string
		supported []string
	}{
		{"DockerCE", &cfg.Versions.DockerCE, DockerCEVersions},
		{"Containerd", &cfg.Versions.Containerd, ContainerdVersions},
		{"Runc", &cfg.Versions.Runc, RuncVersions},
		{"Nerdctl", &cfg.Versions.Nerdctl, NerdctlVersions},
		{"Kubernetes", &cfg.Versions.K8s, K8sVersions},
		{"Kube-OVN", &cfg.Addons.KubeOvn.Version, KubeOvnVersions},
		{"Multus CNI", &cfg.Addons.MultusCNI.Version, MultusCNIVersions},
		{"HAMI", &cfg.Addons.Hami.Version, HamiVersions},
		{"Kube Prometheus Stack", &cfg.Addons.KubePrometheus.Version, KubePrometheusVersions},
	}

	for _, v := range versions {
		if *v.value == "" {
			*v.value = v.supported[0]
			continue
		}
		if !stringInSlice(*v.value, v.supported) {
			return fmt.Errorf("Error: %s version %s is not supported.", v.name, *v.value)
		}
	}

	for i, node := range cfg.Nodes {
		if strings.TrimSpace(node.IP) == "" {
			return fmt.Errorf("Error: Node[%d] ip is required.", i)
		}
		if strings.TrimSpace(node.Password) == "" {
			return fmt.Errorf("Error: Node[%d] password is required.", i)
		}
		if !node.IsMaster {
			cfg.Nodes[i].IsPrimaryMaster = false
		}
	}

	hasMaster := false
	primaryMasterCount := 0
	masterIndices := make([]int, 0)
	for i := range cfg.Nodes {
		if cfg.Nodes[i].IsMaster {
			hasMaster = true
			masterIndices = append(masterIndices, i)
			if cfg.Nodes[i].IsPrimaryMaster {
				primaryMasterCount++
			}
		}
	}

	if cfg.Registry.Endpoint != "" {
		if cfg.Registry.IP == "" {
			return fmt.Errorf("Error: registry ip is required.")
		}
		if cfg.Registry.Port == 0 {
			return fmt.Errorf("Error: registry port is required.")
		}
		if cfg.Registry.Username == "" || cfg.Registry.Password == "" {
			return fmt.Errorf("Error: registry username and password are required.")
		}
	}

	if !hasMaster && cfg.JoinCommand == "" {
		return fmt.Errorf("Error: join command is required.")
	}

	if cfg.HA.Enabled {
		if len(masterIndices) != 3 {
			return fmt.Errorf("Error: HA mode requires exactly 3 master nodes, got %d.", len(masterIndices))
		}
		if primaryMasterCount != 1 {
			return fmt.Errorf("Error: HA mode requires exactly 1 primary master node.")
		}
		if strings.TrimSpace(cfg.HA.VirtualIP) == "" {
			return fmt.Errorf("Error: HA mode requires virtual_ip.")
		}
		for _, idx := range masterIndices {
			if strings.TrimSpace(cfg.Nodes[idx].Interface) == "" {
				return fmt.Errorf("Error: master node[%d] interface is required for HA mode.", idx)
			}
		}
	}

	return nil
}
