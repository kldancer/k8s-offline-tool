package strategy

import (
	"fmt"
	"strings"
)

type UbuntuInstaller struct {
	Ctx *Context
}

func (u *UbuntuInstaller) Name() string            { return "Ubuntu/Debian" }
func (u *UbuntuInstaller) verPath(v string) string { return strings.ReplaceAll(v, ".", "-") }

// --- System Prep ---
func (u *UbuntuInstaller) CheckSELinux() (bool, error) { return true, nil }
func (u *UbuntuInstaller) DisableSELinux() error       { return nil }
func (u *UbuntuInstaller) CheckFirewall() (bool, error) {
	out, _ := u.Ctx.RunCmd("ufw status")
	return strings.Contains(out, "inactive"), nil
}
func (u *UbuntuInstaller) DisableFirewall() error {
	u.Ctx.RunCmd("systemctl stop ufw || true")
	u.Ctx.RunCmd("systemctl disable ufw || true")
	return nil
}
func (u *UbuntuInstaller) CheckSwap() (bool, error) {
	return CheckSwap(u.Ctx)
}
func (u *UbuntuInstaller) DisableSwap() error {
	return DisableSwap(u.Ctx)
}
func (u *UbuntuInstaller) CheckKernelModules() (bool, error) {
	return CheckKernelModules(u.Ctx)
}
func (u *UbuntuInstaller) LoadKernelModules() error {
	return LoadKernelModules(u.Ctx)
}
func (u *UbuntuInstaller) CheckSysctl() (bool, error) {
	return false, nil
}
func (u *UbuntuInstaller) ConfigureSysctl() error {
	return ConfigureSysctl(u.Ctx)
}

// --- Tools ---
func (u *UbuntuInstaller) CheckCommonTools() (bool, error) {
	// 检查一个代表性工具即可
	out, err := u.Ctx.RunCmd("dpkg -l tree")
	return err == nil && strings.Contains(out, "ii"), nil
}
func (u *UbuntuInstaller) InstallCommonTools() error {
	debPath := fmt.Sprintf("%s/common-tools/%s/apt/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s || sudo apt -f install", debPath))
	return err
}

// --- Load Balancer ---
func (u *UbuntuInstaller) CheckHAProxy() (bool, error) {
	out, err := u.Ctx.RunCmd("haproxy -v")
	return err == nil && strings.Contains(strings.ToLower(out), "haproxy"), nil
}

func (u *UbuntuInstaller) InstallHAProxy() error {
	debPath := fmt.Sprintf("%s/ha/haproxy/%s/apt/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s || sudo apt -f install", debPath))
	return err
}

func (u *UbuntuInstaller) CheckKeepalived() (bool, error) {
	out, err := u.Ctx.RunCmd("keepalived -v")
	return err == nil && strings.Contains(strings.ToLower(out), "keepalived"), nil
}

func (u *UbuntuInstaller) InstallKeepalived() error {
	debPath := fmt.Sprintf("%s/ha/keepalived/%s/apt/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s || sudo apt -f install", debPath))
	return err
}

// --- Containerd Granular ---
func (u *UbuntuInstaller) CheckDockerCEPackage() (bool, error) {
	return CheckDockerCEPackage(u.Ctx)
}
func (u *UbuntuInstaller) InstallDockerCEPackage() error {
	return InstallDockerCEBinary(u.Ctx)
}

func (u *UbuntuInstaller) CheckContainerdRunning() (bool, error) {
	return CheckContainerdRunning(u.Ctx)
}
func (u *UbuntuInstaller) ConfigureAndStartContainerd() error {
	return ConfigureAndStartContainerd(u.Ctx)
}

func (u *UbuntuInstaller) CheckConfiguraRegistryContainerd() (bool, error) {
	return CheckConfiguraRegistryContainerd(u.Ctx)
}

func (u *UbuntuInstaller) ConfiguraRegistryContainerd() error {
	return ConfiguraRegistryContainerd(u.Ctx)
}

func (u *UbuntuInstaller) CheckCrictl() (bool, error) {
	return CheckCrictl(u.Ctx)
}
func (u *UbuntuInstaller) ConfigureCrictl() error {
	return ConfigureCrictl(u.Ctx)
}

func (u *UbuntuInstaller) CheckNerdctl() (bool, error) {
	return CheckNerdctl(u.Ctx)
}
func (u *UbuntuInstaller) InstallNerdctl() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.Nerdctl)
	return InstallNerdctl(u.Ctx, vFolder)
}

// --- Accelerators ---
func (u *UbuntuInstaller) CheckAcceleratorConfig() (bool, error) {
	return CheckAcceleratorConfig(u.Ctx)
}

func (u *UbuntuInstaller) ConfigureAccelerator() error {
	if u.Ctx.HasGPU {
		debPath := fmt.Sprintf("%s/common-tools/%s/apt/nvidia-container-toolkit*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch)
		u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
		u.Ctx.RunCmd("nvidia-ctk runtime configure --runtime=containerd")
		// default_runtime_name 改为 nvidia
		u.Ctx.RunCmd("sed -i 's/^\\([[:space:]]*default_runtime_name[[:space:]]*=[[:space:]]*\\)\"runc\"/\\1\"nvidia\"/' /etc/containerd/conf.d/99-nvidia.toml")
		u.Ctx.RunCmd("systemctl restart containerd")
	}

	if u.Ctx.HasNPU {
		return ConfigureNpuContainerRuntime(u.Ctx)
	}

	return nil
}

// --- K8s ---
func (u *UbuntuInstaller) CheckK8sComponents() (bool, error) {
	out, err := u.Ctx.RunCmd("kubeadm version -o short")
	if err != nil {
		return false, nil
	}
	return strings.Contains(out, u.Ctx.Cfg.Versions.K8s), nil
}
func (u *UbuntuInstaller) InstallK8sComponents() error {
	vFolder := u.verPath(u.Ctx.Cfg.Versions.K8s)
	debPath := fmt.Sprintf("%s/k8s/%s/apt/%s/*.deb", u.Ctx.RemoteTmpDir, u.Ctx.Arch, vFolder)
	_, err := u.Ctx.RunCmd(fmt.Sprintf("dpkg -i %s", debPath))
	u.Ctx.RunCmd("systemctl enable --now kubelet")
	u.Ctx.RunCmd("systemctl start kubelet")
	return err
}
