package strategy

import "k8s-offline-tool/pkg/config"

type NodeInstaller interface {
	Name() string

	// System Prep
	CheckSELinux() (bool, error)
	DisableSELinux() error
	CheckFirewall() (bool, error)
	DisableFirewall() error
	CheckSwap() (bool, error)
	DisableSwap() error
	CheckKernelModules() (bool, error)
	LoadKernelModules() error
	CheckSysctl() (bool, error)
	ConfigureSysctl() error

	// Tools
	CheckCommonTools() (bool, error)
	InstallCommonTools() error

	// Load balancer
	CheckHAProxy() (bool, error)
	InstallHAProxy() error
	CheckKeepalived() (bool, error)
	InstallKeepalived() error

	// Containerd Granular Steps
	CheckDockerCEPackage() (bool, error)
	InstallDockerCEPackage() error

	CheckContainerdRunning() (bool, error)
	ConfigureAndStartContainerd() error

	CheckConfiguraRegistryContainerd() (bool, error)
	ConfiguraRegistryContainerd() error

	CheckCrictl() (bool, error)
	ConfigureCrictl() error

	CheckNerdctl() (bool, error)
	InstallNerdctl() error

	// Accelerators
	CheckAcceleratorConfig() (bool, error)
	ConfigureAccelerator() error

	// K8s
	CheckK8sComponents() (bool, error)
	InstallK8sComponents() error
}

type Context struct {
	Cfg           *config.Config
	Arch          string
	SystemName    string
	SystemVersion string
	KernelVersion string
	HasGPU        bool
	HasNPU        bool
	RemoteTmpDir  string
	RunCmd        func(string) (string, error)
}
