package config

var (
	DockerCEVersions   = []string{"29.2.0"}
	ContainerdVersions = []string{"2.2.1"}
	RuncVersions       = []string{"1.3.4"}
	NerdctlVersions    = []string{"2.2.1"}
	K8sVersions        = []string{"1.34.4"}

	KubeOvnVersions        = []string{"1.15.2"}
	MultusCNIVersions      = []string{"snapshot-thick"}
	HamiVersions           = []string{"2.7.1"}
	KubePrometheusVersions = []string{"81.6.0"}
)

var RemoteTmpDir = "/tmp/k8s-offline-install"

const (
	InstallModeFull       = "full"
	InstallModeAddonsOnly = "addons-only"
	InstallModePreInit    = "pre-init"
)

var SupportedInstallModes = []string{InstallModeFull, InstallModeAddonsOnly, InstallModePreInit}

const (
	DefaultPauseImage       = "pause:3.10.1"
	DefaultK8sImageRegistry = "registry.aliyuncs.com"

	DefaultKubeOvnChart             = "kube-ovn-v1.15.2.tgz"
	DefaultHamiChart                = "hami-2.7.1.tgz"
	DefaultHamiWebUIChart           = "hami-webui-1.0.5.tgz"
	DefaultKubePrometheusStackChart = "kube-prometheus-stack-81.6.0.tgz"
	DefaultMultusImage              = "k8snetworkplumbingwg/multus-cni:snapshot-thick"
)
