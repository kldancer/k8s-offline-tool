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

	// Containerd Granular Steps
	CheckContainerdBinaries() (bool, error)
	InstallContainerdBinaries() error

	CheckRunc() (bool, error)
	InstallRunc() error

	CheckContainerdService() (bool, error)
	ConfigureContainerdService() error

	CheckContainerdRunning() (bool, error)
	ConfigureAndStartContainerd() error

	CheckCrictl() (bool, error)
	ConfigureCrictl() error

	CheckNerdctl() (bool, error)
	InstallNerdctl() error

	// GPU
	CheckGPUConfig() (bool, error)
	ConfigureGPU() error

	// K8s
	CheckK8sComponents() (bool, error)
	InstallK8sComponents() error

	// Images
	CheckImages() (bool, error)
	LoadImages() error
}

type Context struct {
	Cfg          *config.Config
	Arch         string
	HasGPU       bool
	RemoteTmpDir string
	RunCmd       func(string) (string, error)
}
