package config

var (
	DockerCEVersions   = []string{"29.2.0"}
	ContainerdVersions = []string{"2.2.1"}
	RuncVersions       = []string{"1.3.4"}
	NerdctlVersions    = []string{"2.2.1"}
	K8sVersions        = []string{"1.35.0"}

	KubeOvnVersions        = []string{"1.15.2"}
	MultusCNIVersions      = []string{"snapshot-thick"}
	HamiVersions           = []string{"2.8.0"}
	KubePrometheusVersions = []string{"81.6.0"}
)

var RemoteTmpDir = "/tmp/k8s-offline-install"

const (
	InstallModeFull        = "full"
	InstallModeAddonsOnly  = "addons-only"
	InstallModeInstallOnly = "install-only"
)

var SupportedInstallModes = []string{InstallModeFull, InstallModeAddonsOnly, InstallModeInstallOnly}

var RequiredKubeOvnImages = []string{
	"docker.io/kubeovn/kube-ovn:v1.15.2",
	"docker.io/kubeovn/vpc-nat-gateway:v1.15.2",
}

var RequiredMultusCNImages = []string{
	"ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick",
}

var RequiredHamiImages = []string{
	"registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:v1.35.0",
	"docker.io/projecthami/hami:v2.8.0",
	"docker.io/jettech/kube-webhook-certgen:v1.5.2",
	"docker.io/liangjw/kube-webhook-certgen:v1.1.1",
	"docker.io/projecthami/mock-device-plugin:1.0.1",
	"ghcr.io/project-hami/hami-dra-monitor:0.1.0",
	"ghcr.io/project-hami/hami-dra-webhook:0.1.0",
	"ghcr.io/projecthami/k8s-dra-driver:v0.0.1-dev",
}

var RequiredKubePrometheusImages = []string{
	"docker.io/busybox:latest",
	"docker.io/rancher/kubectl:v1.35.0",
	"ghcr.io/prometheus-community/windows-exporter:v0.31.3",
	"quay.io/prometheus/alertmanager:v0.31.0",
	"docker.io/grafana/grafana:12.3.2",
	"docker.io/bats/bats:1.13.0",
	"docker.io/curlimages/curl:8.18.0",
	"docker.io/library/busybox:1.37.0",
	"docker.io/grafana/grafana-image-renderer:latest",
	"quay.io/kiwigrid/k8s-sidecar:2.5.0",
	"registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0",
	"quay.io/brancz/kube-rbac-proxy:v0.20.2",
	"quay.io/prometheus/node-exporter:v1.10.2",
	"quay.io/prometheus-operator/admission-webhook:v0.88.1",
	"ghcr.io/jkroepke/kube-webhook-certgen:1.7.4",
	"quay.io/prometheus-operator/prometheus-operator:v0.88.1",
	"quay.io/prometheus-operator/prometheus-config-reloader:v0.88.1",
	"quay.io/thanos/thanos:v0.40.1",
	"quay.io/prometheus/prometheus:v3.9.1",
}

var RequiredK8sImages = []string{
	"registry.aliyuncs.com/google_containers/kube-apiserver:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-controller-manager:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-scheduler:v1.35.0",
	"registry.aliyuncs.com/google_containers/kube-proxy:v1.35.0",
	"registry.aliyuncs.com/google_containers/coredns:v1.13.1",
	"registry.aliyuncs.com/google_containers/pause:3.10.1",
	"registry.aliyuncs.com/google_containers/etcd:3.6.6-0",
}
