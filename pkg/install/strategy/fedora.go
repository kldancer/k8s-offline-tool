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
	f.Ctx.RunCmd("sed -i '/\\/swap.img/s/^/#/' /etc/fstab")
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
	out, err := f.Ctx.RunCmd("rpm -q htop")
	return err == nil && !strings.Contains(out, "not installed"), nil
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
	rpmPath := fmt.Sprintf("%s/ha/haproxy/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

func (f *FedoraInstaller) CheckKeepalived() (bool, error) {
	out, err := f.Ctx.RunCmd("keepalived -v")
	return err == nil && strings.Contains(strings.ToLower(out), "keepalived"), nil
}

func (f *FedoraInstaller) InstallKeepalived() error {
	rpmPath := fmt.Sprintf("%s/ha/keepalived/%s/rpm/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("sudo dnf install -y %s --disablerepo=\"*\" --nogpgcheck", rpmPath))
	return err
}

// --- Containerd Granular ---
func (f *FedoraInstaller) CheckDockerCEPackage() (bool, error) {
	return CheckDockerCEPackage(f.Ctx)
}
func (f *FedoraInstaller) InstallDockerCEPackage() error {
	return InstallDockerCEBinary(f.Ctx)
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

// --- Accelerators ---
func (f *FedoraInstaller) CheckAcceleratorConfig() (bool, error) {
	return CheckAcceleratorConfig(f.Ctx)
}

func (f *FedoraInstaller) ConfigureAccelerator() error {
	if f.Ctx.HasGPU {
		rpmPath := fmt.Sprintf("%s/common-tools/%s/rpm/nvidia-container-toolkit*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch)
		f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
		f.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")

		// default_runtime_name 改为 nvidia
		f.Ctx.RunCmd("sed -i 's/^\\([[:space:]]*default_runtime_name[[:space:]]*=[[:space:]]*\\)\"runc\"/\\1\"nvidia\"/' /etc/containerd/conf.d/99-nvidia.toml")
		f.Ctx.RunCmd("systemctl restart containerd")
	}

	if f.Ctx.HasNPU {
		arch := f.Ctx.Arch
		if arch == "x86_64" {
			arch = "amd64"
		} else if arch == "aarch64" {
			arch = "arm64"
		}

		runtimeDir := fmt.Sprintf("%s/docker-runtime/ascend/%s", f.Ctx.RemoteTmpDir, arch)
		installCmd := fmt.Sprintf("cd %s && ./*.run --install", runtimeDir)
		if _, err := f.Ctx.RunCmd(installCmd); err != nil {
			return fmt.Errorf("failed to install ascend docker runtime: %v", err)
		}

		// 然后还要通过cat /etc/containerd/config.toml | grep ascend-docker-runtime 验证一下输出，没问题才systemctl restart containerd
		out, err := f.Ctx.RunCmd("cat /etc/containerd/config.toml | grep ascend-docker-runtime")
		if err != nil || !strings.Contains(out, "ascend-docker-runtime") {
			return fmt.Errorf("failed to verify ascend docker runtime installation: %v", err)
		}
		f.Ctx.RunCmd("systemctl restart containerd")
	}

	return nil
}

// --- K8s ---
func (f *FedoraInstaller) CheckK8sComponents() (bool, error) {
	out, err := f.Ctx.RunCmd("kubeadm version -o short")
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, f.Ctx.Cfg.Versions.K8s), nil
}
func (f *FedoraInstaller) InstallK8sComponents() error {
	vFolder := f.verPath(f.Ctx.Cfg.Versions.K8s)
	// Path example: k8s/amd64/rpm/1-34-4/*.rpm
	rpmPath := fmt.Sprintf("%s/k8s/%s/rpm/%s/*.rpm", f.Ctx.RemoteTmpDir, f.Ctx.Arch, vFolder)
	_, err := f.Ctx.RunCmd(fmt.Sprintf("rpm -Uvh %s --nodeps --force", rpmPath))
	f.Ctx.RunCmd("systemctl enable --now kubelet")
	f.Ctx.RunCmd("systemctl start kubelet")
	return err
}
