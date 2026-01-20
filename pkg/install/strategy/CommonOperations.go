package strategy

import (
	"fmt"
	"strings"
)

// --- System Prep ---
func CheckSwap(ctx *Context) (bool, error) {
	out, _ := ctx.RunCmd("swapon --show")
	return strings.TrimSpace(out) == "", nil
}

func CheckKernelModules(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("cat /etc/modules-load.d/containerd.conf")
	return err == nil && strings.Contains(out, "overlay"), nil
}

func LoadKernelModules(ctx *Context) error {
	ctx.RunCmd(`cat > /etc/modules-load.d/containerd.conf << EOF
overlay
br_netfilter
EOF`)
	ctx.RunCmd("modprobe overlay")
	ctx.RunCmd("modprobe br_netfilter")
	return nil
}

func CheckSysctl(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("cat /etc/sysctl.d/99-kubernetes-cri.conf")
	return err == nil && strings.Contains(out, "net.ipv4.ip_forward"), nil
}

func ConfigureSysctl(ctx *Context) error {
	ctx.RunCmd(`cat > /etc/sysctl.d/99-kubernetes-cri.conf << EOF
net.bridge.bridge-nf-call-iptables  = 1
net.ipv4.ip_forward                 = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF`)
	ctx.RunCmd("sysctl --system")
	return nil
}

// --- Containerd Granular ---
func CheckContainerdBinaries(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("containerd --version")
	// 检查版本是否包含配置的版本号
	return err == nil && strings.Contains(out, ctx.Cfg.Versions.Containerd), nil
}
func InstallContainerdBinaries(ctx *Context, vFolder string) error {
	tarCmd := fmt.Sprintf("tar -C /usr/local -xzf %s/containerd/%s/%s/containerd-%s-linux-%s.tar.gz",
		ctx.RemoteTmpDir, ctx.Arch, vFolder, ctx.Cfg.Versions.Containerd, ctx.Arch)
	_, err := ctx.RunCmd(tarCmd)
	return err
}

func CheckRunc(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("runc --version")
	return err == nil && strings.Contains(out, ctx.Cfg.Versions.Runc), nil
}

func InstallRunc(ctx *Context, vFolder string) error {
	runcPath := fmt.Sprintf("%s/runc/%s/%s/runc.%s", ctx.RemoteTmpDir, ctx.Arch, vFolder, ctx.Arch)
	_, err := ctx.RunCmd(fmt.Sprintf("install -m 0755 %s /usr/local/bin/runc", runcPath))
	return err
}

func CheckContainerdService(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("test -e /usr/lib/systemd/system/containerd.service && echo EXISTS || echo MISSING")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "EXISTS", nil
}

func ConfigureContainerdService(ctx *Context) error {
	svcSrc := fmt.Sprintf("%s/containerd/containerd.service", ctx.RemoteTmpDir)
	_, err := ctx.RunCmd(fmt.Sprintf("cp %s /usr/lib/systemd/system/containerd.service", svcSrc))
	return err
}

func CheckContainerdRunning(ctx *Context) (bool, error) {
	out, _ := ctx.RunCmd("systemctl is-active containerd")
	return strings.TrimSpace(out) == "active", nil
}

func ConfigureAndStartContainerd(ctx *Context) error {
	// 1. 生成默认配置
	ctx.RunCmd("mkdir -p /etc/containerd")
	ctx.RunCmd("containerd config default > /etc/containerd/config.toml")

	// 2. 修改 SystemdCgroup
	ctx.RunCmd("sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml")

	// 3. 配置 Registry
	if ctx.Cfg.Registry.Endpoint != "" {
		// 3.1 启用 certs.d 目录配置
		ctx.RunCmd("sed -i \"s|config_path = '/etc/containerd/certs.d:/etc/docker/certs.d'|config_path = '/etc/containerd/certs.d'|g\" /etc/containerd/config.toml")
		// 3.2 配置 hosts.toml
		regDomain := ctx.Cfg.Registry.Endpoint + fmt.Sprintf(":%d", ctx.Cfg.Registry.Port)

		// 3.3 修改 sandbox镜像
		c := fmt.Sprintf("sed -i \"s|sandbox = 'registry.k8s.io/pause:3.10.1'|sandbox = '%s/google_containers/pause:3.10.1'|g\" /etc/containerd/config.toml", regDomain)
		ctx.RunCmd(c)

		// 3.4 添加域名解析配置
		ctx.RunCmd(fmt.Sprintf(" echo \"%s %s\" | sudo tee -a /etc/hosts", ctx.Cfg.Registry.IP, ctx.Cfg.Registry.Endpoint))

		regUrl := "http://" + regDomain

		// 创建目录
		ctx.RunCmd(fmt.Sprintf("mkdir -p /etc/containerd/certs.d/%s", regDomain))

		// 写入 hosts.toml
		hostsToml := fmt.Sprintf(`server = "%s"

[host."%s"]
  capabilities = ["pull", "resolve", "push"]
`, regUrl, regUrl)

		cmd := fmt.Sprintf("cat > /etc/containerd/certs.d/%s/hosts.toml <<EOF\n%s\nEOF", regDomain, hostsToml)
		if _, err := ctx.RunCmd(cmd); err != nil {
			return fmt.Errorf("failed to write hosts.toml: %v", err)
		}

	}

	// 4. 启动服务
	ctx.RunCmd("systemctl daemon-reload")
	_, err := ctx.RunCmd("systemctl enable --now containerd")
	return err
}

func CheckCrictl(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("cat /etc/crictl.yaml")
	return err == nil && strings.Contains(out, "containerd.sock"), nil
}

func ConfigureCrictl(ctx *Context) error {
	cmd := `cat > /etc/crictl.yaml << EOF
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
timeout: 2
debug: false
pull-image-on-create: false
EOF`
	_, err := ctx.RunCmd(cmd)
	return err
}

func CheckNerdctl(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("nerdctl --version")
	return err == nil && strings.Contains(out, ctx.Cfg.Versions.Nerdctl), nil
}

func InstallNerdctl(ctx *Context, vFolder string) error {
	tarPath := fmt.Sprintf("%s/nerdctl/%s/%s/nerdctl-%s-linux-%s.tar.gz",
		ctx.RemoteTmpDir, ctx.Arch, vFolder, ctx.Cfg.Versions.Nerdctl, ctx.Arch)
	_, err := ctx.RunCmd(fmt.Sprintf("tar -xzf %s -C /usr/local/bin/", tarPath))
	return err
}

// --- GPU ---
func CheckGPUConfig(ctx *Context) (bool, error) {
	if !ctx.HasGPU {
		return true, nil
	}
	_, err := ctx.RunCmd("nvidia-container-cli info")
	return err == nil, nil
}
