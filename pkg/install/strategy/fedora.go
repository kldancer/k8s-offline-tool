package strategy

import (
	"fmt"
	"strings"
)

type FedoraInstaller struct {
	Ctx *Context
}

func (f *FedoraInstaller) Name() string            { return "Fedora/CentOS" }
func (f *FedoraInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (f *FedoraInstaller) CheckSELinux() (bool, error) {
	out, _ := f.Ctx.RunCmd("getenforce")
	return strings.Contains(strings.ToLower(out), "disabled") || strings.Contains(strings.ToLower(out), "permissive"), nil
}
func (f *FedoraInstaller) DisableSELinux() error {
	f.Ctx.RunCmd("sed -ri 's/SELINUX=enforcing/SELINUX=disabled/' /etc/selinux/config")
	f.Ctx.RunCmd("setenforce 0 || true")
	return nil
}
func (f *FedoraInstaller) CheckFirewall() (bool, error) {
	out, _ := f.Ctx.RunCmd("systemctl is-active firewalld")
	return strings.TrimSpace(out) == "inactive" || strings.Contains(out, "unknown"), nil
}
func (f *FedoraInstaller) DisableFirewall() error {
	f.Ctx.RunCmd("systemctl stop firewalld || true")
	f.Ctx.RunCmd("systemctl disable firewalld || true")
	return nil
}
func (f *FedoraInstaller) CheckSwap() (bool, error) {
	return CheckSwap(f.Ctx)
}
func (f *FedoraInstaller) DisableSwap() error {
	f.Ctx.RunCmd("dnf remove -y zram-generator-defaults || true")
	f.Ctx.RunCmd("swapoff -a")
	return nil
}
func (f *FedoraInstaller) CheckKernelModules() (bool, error) {
	return CheckKernelModules(f.Ctx)
}
func (f *FedoraInstaller) LoadKernelModules() error {
	return LoadKernelModules(f.Ctx)
}
func (f *FedoraInstaller) CheckSysctl() (bool, error) {
	return CheckSysctl(f.Ctx)
}
func (f *FedoraInstaller) ConfigureSysctl() error {
	return ConfigureSysctl(f.Ctx)
}

// --- Tools ---
func (f *FedoraInstaller) CheckCommonTools() (bool, error) {
	//if _, err := f.Ctx.RunCmd("rpm -q htop"); err != nil {
	//	return false, nil
	//}
	// 暂时默认重新装一遍就行
	return false, nil
}
func (f *FedoraInstaller) InstallCommonTools() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Load Balancer ---
func (f *FedoraInstaller) CheckHAProxy() (bool, error) {
	out, err := f.Ctx.RunCmd("haproxy -v")
	return err == nil && strings.Contains(strings.ToLower(out), "haproxy"), nil
}

func (f *FedoraInstaller) InstallHAProxy() error {
	rpmPath := fmt.Sprintf("%s/haproxy/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

func (f *FedoraInstaller) CheckKeepalived() (bool, error) {
	out, err := f.Ctx.RunCmd("keepalived -v")
	return err == nil && strings.Contains(strings.ToLower(out), "keepalived"), nil
}

func (f *FedoraInstaller) InstallKeepalived() error {
	rpmPath := fmt.Sprintf("%s/keepalived/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Containerd Granular ---
func (f *FedoraInstaller) CheckContainerdPackage() (bool, error) {
	return CheckContainerdPackage(f.Ctx)
}
func (f *FedoraInstaller) InstallContainerdPackage() error {
	rpmPath := fmt.Sprintf("%s/docker-ce/%s/rpm/containerd.io-*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

func (f *FedoraInstaller) CheckContainerdRunning() (bool, error) {
	return CheckContainerdRunning(f.Ctx)
}

func (f *FedoraInstaller) ConfigureAndStartContainerd() error {
	return ConfigureAndStartContainerd(f.Ctx)
}

func (f *FedoraInstaller) CheckConfiguraRegistryContainerd() (bool, error) {
	return CheckConfiguraRegistryContainerd(f.Ctx)
}

func (f *FedoraInstaller) ConfiguraRegistryContainerd() error {
	return ConfiguraRegistryContainerd(f.Ctx)
}

func (f *FedoraInstaller) CheckCrictl() (bool, error) {
	return CheckCrictl(f.Ctx)
}

func (f *FedoraInstaller) ConfigureCrictl() error {
	return ConfigureCrictl(f.Ctx)
}

func (f *FedoraInstaller) CheckNerdctl() (bool, error) {
	return CheckNerdctl(f.Ctx)
}

func (f *FedoraInstaller) InstallNerdctl() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.Nerdctl)
	return InstallNerdctl(f.Ctx, vFolder)
}

// --- GPU ---
func (f *FedoraInstaller) CheckGPUConfig() (bool, error) {
	return CheckGPUConfig(f.Ctx)
}

func (f *FedoraInstaller) ConfigureGPU() error {
	rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/nvidia-container-toolkit*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	f.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
	f.Ctx.RunCmd("systemctl restart containerd")
	return nil
}

// --- K8s ---
func (f *FedoraInstaller) CheckK8sComponents() (bool, error) {
	// 暂时不检查，直接覆盖安装
	return false, nil
}
func (f *FedoraInstaller) InstallK8sComponents() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.K8s)
	// Path example: k8s/1-35-0/rpm/1-35-0/*.rpm
	rpmPath := fmt.Sprintf("%s/k8s/%s/rpm/%s/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	f.Ctx.RunCmd("systemctl enable --now kubelet")
	f.Ctx.RunCmd("systemctl start kubelet")
	return err
}
