package config

var (
	ContainerdVersions = []string{"2.2.1"}
	RuncVersions       = []string{"1.3.4"}
	NerdctlVersions    = []string{"2.2.1"}
	K8sVersions        = []string{"1.35.0"}

	KubeOvnVersions          = []string{"1.15.0"}
	MultusCNIVersions        = []string{"snapshot-thick"}
	LocalPathStorageVersions = []string{"0.0.34"}
)

var RemoteTmpDir = "/tmp/k8s-offline-install"

const (
	InstallModeFull       = "full"
	InstallModeAddonsOnly = "addons-only"
)

var SupportedInstallModes = []string{InstallModeFull, InstallModeAddonsOnly}

var RequiredImages = []string{
	"registry.aliyuncs.com/google_containers/kube-apiserver:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-controller-manager:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-scheduler:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-proxy:v1.35.0",
	"registry.aliyuncs.com/google_containers/coredns:v1.13.1",
	"registry.aliyuncs.com/google_containers/pause:3.10.1",
	"registry.aliyuncs.com/google_containers/etcd:3.6.6-0",
	"docker.io/kubeovn/kube-ovn:v1.15.0",
	"docker.io/kubeovn/vpc-nat-gateway:v1.15.0",
	"ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick",
	"rancher/local-path-provisioner:v0.0.34",
}
