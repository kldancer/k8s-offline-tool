package strategy

import (
	"fmt"
	"strings"
)

type OpenEulerInstaller struct {
	Ctx *Context
}

func (o *OpenEulerInstaller) Name() string            { return "openEuler" }
func (o *OpenEulerInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (o *OpenEulerInstaller) CheckSELinux() (bool, error) {
	out, _ := o.Ctx.RunCmd("getenforce")
	return strings.Contains(strings.ToLower(out), "disabled") || strings.Contains(strings.ToLower(out), "permissive"), nil
}
func (o *OpenEulerInstaller) DisableSELinux() error {
	o.Ctx.RunCmd("sed -ri 's/SELINUX=enforcing/SELINUX=disabled/' /etc/selinux/config")
	o.Ctx.RunCmd("setenforce 0 || true")
	return nil
}
func (o *OpenEulerInstaller) CheckFirewall() (bool, error) {
	out, _ := o.Ctx.RunCmd("systemctl is-active firewalld")
	return strings.TrimSpace(out) == "inactive" || strings.Contains(out, "unknown"), nil
}
func (o *OpenEulerInstaller) DisableFirewall() error {
	o.Ctx.RunCmd("systemctl stop firewalld || true")
	o.Ctx.RunCmd("systemctl disable firewalld || true")
	return nil
}
func (o *OpenEulerInstaller) CheckSwap() (bool, error) {
	return CheckSwap(o.Ctx)
}
func (o *OpenEulerInstaller) DisableSwap() error {
	o.Ctx.RunCmd("swapoff -a")
	o.Ctx.RunCmd("sed -i '/swap/s/^/#/' /etc/fstab")
	return nil
}
func (o *OpenEulerInstaller) CheckKernelModules() (bool, error) {
	return CheckKernelModules(o.Ctx)
}
func (o *OpenEulerInstaller) LoadKernelModules() error {
	return LoadKernelModules(o.Ctx)
}
func (o *OpenEulerInstaller) CheckSysctl() (bool, error) {
	return CheckSysctl(o.Ctx)
}
func (o *OpenEulerInstaller) ConfigureSysctl() error {
	return ConfigureSysctl(o.Ctx)
}

// --- Tools ---
func (o *OpenEulerInstaller) CheckCommonTools() (bool, error) {
	out, err := o.Ctx.RunCmd("rpm -q htop")
	return err == nil && !strings.Contains(out, "not installed"), nil
}
func (o *OpenEulerInstaller) InstallCommonTools() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Load Balancer ---
func (o *OpenEulerInstaller) CheckHAProxy() (bool, error) {
	out, err := o.Ctx.RunCmd("haproxy -v")
	return err == nil && strings.Contains(strings.ToLower(out), "haproxy"), nil
}

func (o *OpenEulerInstaller) InstallHAProxy() error {
	rpmPath := fmt.Sprintf("%s/ha/haproxy/%s/rpm/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

func (o *OpenEulerInstaller) CheckKeepalived() (bool, error) {
	out, err := o.Ctx.RunCmd("keepalived -v")
	return err == nil && strings.Contains(strings.ToLower(out), "keepalived"), nil
}

func (o *OpenEulerInstaller) InstallKeepalived() error {
	rpmPath := fmt.Sprintf("%s/ha/keepalived/%s/rpm/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Containerd Granular ---
func (o *OpenEulerInstaller) CheckDockerCEPackage() (bool, error) {
	return CheckDockerCEPackage(o.Ctx)
}
func (o *OpenEulerInstaller) InstallDockerCEPackage() error {
	return InstallDockerCEBinary(o.Ctx)
}

func (o *OpenEulerInstaller) CheckContainerdRunning() (bool, error) {
	return CheckContainerdRunning(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureAndStartContainerd() error {
	return ConfigureAndStartContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) CheckConfiguraRegistryContainerd() (bool, error) {
	return CheckConfiguraRegistryContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) ConfiguraRegistryContainerd() error {
	return ConfiguraRegistryContainerd(o.Ctx)
}

func (o *OpenEulerInstaller) CheckCrictl() (bool, error) {
	return CheckCrictl(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureCrictl() error {
	return ConfigureCrictl(o.Ctx)
}

func (o *OpenEulerInstaller) CheckNerdctl() (bool, error) {
	return CheckNerdctl(o.Ctx)
}

func (o *OpenEulerInstaller) InstallNerdctl() error {
	vFolder := o.verPath(o.Ctx.Cfg.Versions.Nerdctl)
	return InstallNerdctl(o.Ctx, vFolder)
}

// --- GPU ---
func (o *OpenEulerInstaller) CheckGPUConfig() (bool, error) {
	return CheckGPUConfig(o.Ctx)
}

func (o *OpenEulerInstaller) ConfigureGPU() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/nvidia-container-toolkit*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch)
	o.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	o.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
	o.Ctx.RunCmd("systemctl restart containerd")
	return nil
}

// --- K8s ---
func (o *OpenEulerInstaller) CheckK8sComponents() (bool, error) {
	out, err := o.Ctx.RunCmd("kubeadm version -o short")
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, o.Ctx.Cfg.Versions.K8s), nil
}
func (o *OpenEulerInstaller) InstallK8sComponents() error {
	vFolder := o.verPath(o.Ctx.Cfg.Versions.K8s)
	rpmPath := fmt.Sprintf("%s/k8s/%s/rpm/%s/*.rpm", o.Ctx.RemoteTmpDir, o.Ctx.Arch, vFolder)
	_, err := o.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	o.Ctx.RunCmd("systemctl enable --now kubelet")
	o.Ctx.RunCmd("systemctl start kubelet")
	return err
}
