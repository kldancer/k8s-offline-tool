package strategy

import (
	"encoding/base64"
	"fmt"
	"k8s-offline-tool/pkg/config"
	"strings"
)

// --- System Prep ---
func CheckSwap(ctx *Context) (bool, error) {
	out, _ := ctx.RunCmd("swapon --show")
	return strings.TrimSpace(out) == "", nil
}

func DisableSwap(ctx *Context) error {
	ctx.RunCmd("swapoff -a")
	ctx.RunCmd("sed -i '/\\/swap.img/s/^/#/' /etc/fstab")
	return nil
}

func CheckKernelModules(ctx *Context) (bool, error) {
	out, err := ctx.RunCmd("cat /etc/modules-load.d/containerd.conf")
	return err == nil && strings.Contains(out, "overlay") && strings.Contains(out, "br_netfilter"), nil
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

func ConfigureSysctl(ctx *Context) error {
	ctx.RunCmd(`cat > /etc/sysctl.d/99-kubernetes-tool.conf << EOF
net.bridge.bridge-nf-call-iptables  = 1
net.ipv4.ip_forward                 = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF`)
	ctx.RunCmd(`sed -ri '/^[[:space:]]*net\.ipv4\.ip_forward[[:space:]]*=/d' /etc/sysctl.d/99-sysctl.conf &&
echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.d/99-sysctl.conf`)
	ctx.RunCmd(" sysctl -p 99-kubernetes-tool.conf")
	return nil
}

// --- Containerd Granular ---
func CheckDockerCEPackage(ctx *Context) (bool, error) {
	containerdOut, err := ctx.RunCmd("containerd --version")
	if err != nil {
		return false, nil
	}
	dockerOut, err := ctx.RunCmd("docker --version")
	if err != nil {
		return false, nil
	}
	runcOut, err := ctx.RunCmd("runc --version")
	if err != nil {
		return false, nil
	}
	return strings.Contains(dockerOut, ctx.Cfg.Versions.DockerCE) &&
		strings.Contains(containerdOut, ctx.Cfg.Versions.Containerd) &&
		strings.Contains(runcOut, ctx.Cfg.Versions.Runc), nil
}

func InstallDockerCEBinary(ctx *Context) error {
	vDocker := strings.ReplaceAll(ctx.Cfg.Versions.DockerCE, ".", "-")
	vContainerd := strings.ReplaceAll(ctx.Cfg.Versions.Containerd, ".", "-")
	vRunc := strings.ReplaceAll(ctx.Cfg.Versions.Runc, ".", "-")

	// 1. Install Docker
	// Path: docker-ce/docker/arm64/29-2-0/docker-29.2.0.tgz
	dockerTar := fmt.Sprintf("%s/docker-ce/docker/%s/%s/docker-%s.tgz", ctx.RemoteTmpDir, ctx.Arch, vDocker, ctx.Cfg.Versions.DockerCE)
	ctx.RunCmd(fmt.Sprintf("tar -xzf %s -C /usr/local/src", dockerTar))
	ctx.RunCmd("cp -f /usr/local/src/docker/* /usr/local/bin/")

	// 2. Install Containerd
	// Path: docker-ce/containerd/arm64/2-2-1/containerd-2.2.1-linux-arm64.tar.gz
	containerdTar := fmt.Sprintf("%s/docker-ce/containerd/%s/%s/containerd-%s-linux-%s.tar.gz", ctx.RemoteTmpDir, ctx.Arch, vContainerd, ctx.Cfg.Versions.Containerd, ctx.Arch)
	ctx.RunCmd(fmt.Sprintf("tar -C /usr/local -xzf %s", containerdTar))

	// 3. Install Runc
	// Path: docker-ce/runc/arm64/1-3-4/runc.arm64
	runcBin := fmt.Sprintf("%s/docker-ce/runc/%s/%s/runc.%s", ctx.RemoteTmpDir, ctx.Arch, vRunc, ctx.Arch)
	ctx.RunCmd(fmt.Sprintf("install -m 755 %s /usr/local/sbin/runc", runcBin))

	// 4. Create necessary directories
	ctx.RunCmd("mkdir -p /etc/docker /var/lib/docker /run/containerd")

	// 5. Create systemd units
	dockerService := `[Unit]
Description=Docker Application Container Engine
Documentation=https://docs.docker.com
After=network-online.target containerd.service
Requires=containerd.service

[Service]
Type=notify
ExecStart=/usr/local/bin/dockerd --containerd=/run/containerd/containerd.sock
Restart=always
RestartSec=5
LimitNOFILE=infinity
LimitNPROC=infinity
LimitCORE=infinity
Delegate=yes
KillMode=process

[Install]
WantedBy=multi-user.target
`
	containerdService := `[Unit]
Description=containerd container runtime
Documentation=https://containerd.io
After=network.target

[Service]
ExecStart=/usr/local/bin/containerd
Restart=always
RestartSec=5
Delegate=yes
KillMode=process
LimitNOFILE=infinity
LimitNPROC=infinity
LimitCORE=infinity

[Install]
WantedBy=multi-user.target
`
	ctx.RunCmd(fmt.Sprintf("cat > /etc/systemd/system/docker.service <<EOF\n%s\nEOF", dockerService))
	ctx.RunCmd(fmt.Sprintf("cat > /etc/systemd/system/containerd.service <<EOF\n%s\nEOF", containerdService))

	// 6. Reload
	ctx.RunCmd("systemctl daemon-reload")
	return nil
}

func CheckContainerdRunning(ctx *Context) (bool, error) {
	// 不必检查，直接覆盖执行即可
	return false, nil
}

func ConfigureAndStartContainerd(ctx *Context) error {
	// 1. 生成默认配置
	ctx.RunCmd("mkdir -p /etc/containerd")
	ctx.RunCmd("containerd config default > /etc/containerd/config.toml")

	// 2. 修改 SystemdCgroup
	ctx.RunCmd("sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml")

	// 3. 修改 sandbox镜像
	c := fmt.Sprintf("sed -i \"s|sandbox = 'registry.k8s.io/%s'|sandbox = '%s/google_containers/%s'|g\" /etc/containerd/config.toml", config.DefaultPauseImage, config.DefaultK8sImageRegistry, config.DefaultPauseImage)
	ctx.RunCmd(c)

	// 3. 启动服务
	ctx.RunCmd("systemctl daemon-reload")
	ctx.RunCmd("systemctl enable --now containerd || true")
	//_, err := ctx.RunCmd("systemctl enable --now docker || true")
	return nil
}

func CheckConfiguraRegistryContainerd(ctx *Context) (bool, error) {
	// 不必检查，直接覆盖执行即可
	return false, nil
}

func ConfiguraRegistryContainerd(ctx *Context) error {
	// 1.1 启用 certs.d 目录配置
	ctx.RunCmd("sed -i \"s|config_path = '/etc/containerd/certs.d:/etc/docker/certs.d'|config_path = '/etc/containerd/certs.d'|g\" /etc/containerd/config.toml")
	// 1.2 配置 hosts.toml
	regDomain := ctx.Cfg.Registry.Endpoint + fmt.Sprintf(":%d", ctx.Cfg.Registry.Port)

	// 1.3 修改 sandbox镜像
	c := fmt.Sprintf("sed -i \"s|sandbox = '%s/google_containers/%s'|sandbox = '%s/google_containers/%s'|g\" /etc/containerd/config.toml", config.DefaultK8sImageRegistry, config.DefaultPauseImage, regDomain, config.DefaultPauseImage)
	ctx.RunCmd(c)

	// 1.4 添加域名解析配置
	ctx.RunCmd(fmt.Sprintf(" echo \"%s %s\" | sudo tee -a /etc/hosts", ctx.Cfg.Registry.IP, ctx.Cfg.Registry.Endpoint))

	regUrl := "http://" + regDomain
	regAuth := base64.StdEncoding.EncodeToString([]byte(ctx.Cfg.Registry.Username + ":" + ctx.Cfg.Registry.Password))

	// 创建目录
	ctx.RunCmd(fmt.Sprintf("mkdir -p /etc/containerd/certs.d/%s", regDomain))

	// 写入 hosts.toml
	hostsToml := fmt.Sprintf(`server = "%s"

[host."%s"]
  capabilities = ["pull", "resolve", "push"]

[host."%s".header]
  authorization = "Basic %s"
`, regUrl, regUrl, regUrl, regAuth)

	cmd := fmt.Sprintf("cat > /etc/containerd/certs.d/%s/hosts.toml <<EOF\n%s\nEOF", regDomain, hostsToml)
	if _, err := ctx.RunCmd(cmd); err != nil {
		return fmt.Errorf("failed to write hosts.toml: %v", err)
	}
	// 4. 重启服务
	ctx.RunCmd("systemctl daemon-reload")
	_, err := ctx.RunCmd("systemctl restart containerd")
	return err
}

func CheckCrictl(ctx *Context) (bool, error) {
	// 不必检查，直接覆盖执行即可
	return false, nil
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

// --- Accelerators ---
func CheckAcceleratorConfig(ctx *Context) (bool, error) {
	if ctx.HasGPU {
		if out, err := ctx.RunCmd("test -e /etc/containerd/conf.d/99-nvidia.toml && echo EXISTS || echo MISSING"); err != nil || strings.TrimSpace(out) == "MISSING" {
			return false, err
		}
		if _, err := ctx.RunCmd("nvidia-container-cli info"); err != nil {
			return false, err
		}
	}

	if ctx.HasNPU {
		out, err := ctx.RunCmd("cat /etc/containerd/config.toml.rpmsave | grep ascend-docker-runtime || true")
		if err != nil || !strings.Contains(out, "ascend-docker-runtime") {
			return false, err
		}
	}

	return true, nil
}

func ConfigureNpuContainerRuntime(ctx *Context) error {
	runtimeDir := fmt.Sprintf("%s/docker-runtime/ascend/%s", ctx.RemoteTmpDir, ctx.Arch)

	ctx.RunCmd(fmt.Sprintf("chmod u+x %s/*.run", runtimeDir))

	installCmd := fmt.Sprintf("cd %s && ./*.run --install", runtimeDir)
	if _, err := ctx.RunCmd(installCmd); err != nil {
		return fmt.Errorf("failed to install ascend docker runtime: %v", err)
	}

	out, err := ctx.RunCmd("cat /etc/containerd/config.toml.rpmsave | grep ascend-docker-runtime")
	if err != nil || !strings.Contains(out, "ascend-docker-runtime") {
		return fmt.Errorf("failed to verify ascend docker runtime installation: %v", err)
	}
	ctx.RunCmd("systemctl restart containerd")
	return nil
}
