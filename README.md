# k8s-offline-tool

该项目用于在离线或内网环境中安装 Kubernetes，并可在已有集群中部署常用组件（CNI 与存储）。工具通过 SSH 连接目标节点，分发离线资源并执行安装/部署步骤。

## 功能概览

- 离线安装基础组件：linux通用工具包、containerd、runc、nerdctl、kubeadm/kubelet/kubectl。
- 在 master 节点初始化集群，并自动生成 worker 的 join command，若配置中没有master节点，需手动配置worker 节点的 join command。
- 支持私有镜像仓库：同步所需镜像到私有 registry，并在部署时重写镜像地址。前提：程序执行的本地环境以配置能访问该私有仓库。
- 支持预检查模式，检查各安装步骤是否需要执行，不执行安装动作。
- 支持安装模式选择，从零安装并初始化集群还是在已有集群中仅部署k8s组件，组件部署：kube-ovn、multus-cni、local-path-storage（可选）。。

## 配置说明

### 全量配置示例

```yaml
# 全局 SSH 默认设置
ssh_port: 22
user: "root"

# 命令执行超时（秒）
command_timeout_seconds: 600

# 安装模式：
# - full: 从零安装并初始化集群
# - addons-only: 在已有集群中仅部署k8s组件
install_mode: "full"

# 软件版本定义
versions:
  containerd: "2.2.1"
  runc: "1.3.4"
  nerdctl: "2.2.1"
  k8s: "1.35.0"

# 组件部署配置（默认不启用）
addons:
  kube_ovn:
    enabled: false
    version: "1.15.0"
  multus_cni:
    enabled: false
    version: "snapshot-thick"
  local_path_storage:
    enabled: false
    version: "0.0.34"

# 仅执行预检查，不执行安装动作
dry_run: true

# 私有仓库配置（可选）
registry:
  endpoint: "ykl.io"
  port: 40443
  ip: 192.168.31.175

# 节点列表（按顺序进行安装）
nodes:
  - ip: "192.168.1.8"
    password: "root"
    ssh_port: 22
    is_master: true
    install_tools: true
  - ip: "192.168.1.10"
    password: "root"
    ssh_port: 22
    is_master: false
    install_tools: true

# Worker 节点加入集群的命令 (在 is_master: false 的节点上执行)
join_command: "kubeadm join 192.168.1.10:6443 --token <token> --discovery-token-ca-cert-hash <hash>"
```
极简配置示例见 [config-minimalism.yaml](example/config-minimalism.yaml)

### 字段解释与默认值

#### 顶层字段

| 字段 | 必填 | 默认值    | 说明 |
| -- | --- |--------| --- |
| `ssh_port` | 否 | `22`   | SSH 端口默认值，可被节点级配置覆盖。 |
| `user` | 否 | `root` | SSH 用户名。 |
| `command_timeout_seconds` | 否 | `600`  | 远程命令执行超时（秒）。 |
| `install_mode` | 否 | `full` | 安装模式：`full` 为从零安装集群，`addons-only` 为仅部署组件。 |
| `dry_run` | 否 | `false` | 仅执行预检查，不执行安装动作。 |
| `versions` | 否 | 见下表    | 离线包版本配置。 |
| `addons` | 否 | 见下表    | 组件启用与版本配置。 |
| `registry` | 否 | 空      | 私有镜像仓库配置，启用后会同步镜像并重写部署文件。 |
| `nodes` | 是 | 见下表    | 节点列表，至少包含一个 `is_master: true` 的节点。 |
| `join_command` | 否 | 空      | worker 加入集群时使用的命令。若未指定，会在 master 初始化后自动生成。 |

#### `versions`（支持版本）

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `containerd` | `2.2.1` | containerd 版本。 |
| `runc` | `1.3.4` | runc 版本。 |
| `nerdctl` | `2.2.1` | nerdctl 版本。 |
| `k8s` | `1.35.0` | Kubernetes 版本。 |

#### `addons`（支持版本）
后续持续添加适配其他必要组件

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `kube_ovn.enabled` | `false` | 是否部署 kube-ovn。 |
| `kube_ovn.version` | `1.15.0` | kube-ovn 版本。 |
| `multus_cni.enabled` | `false` | 是否部署 multus-cni。 |
| `multus_cni.version` | `snapshot-thick` | multus-cni 版本。 |
| `local_path_storage.enabled` | `false` | 是否部署 local-path-storage。 |
| `local_path_storage.version` | `0.0.34` | local-path-storage 版本。 |

#### `registry`

| 字段 | 必填 | 说明                            |
| --- |----|-------------------------------|
| `endpoint` | 是  | 私有镜像仓库域名（http）。               |
| `port` | 是  | 私有镜像仓库端口。                     |
| `ip` | 是  | 私有镜像仓库的 IP，用于写入 `/etc/hosts`。 |


#### `nodes`
| 字段 | 必填 | 默认值  | 说明|
| --- |----|------|---|
| `ip` | 是  | -    | 节点 IP |
| `password` | 是  | -    | 节点登录密码。             |
| `ssh_port` | 否  | 22   | SSH 端口，默认为 `22`。    |
| `is_master` | 否  | true | 是否为 master 节点。      |
| `install_tools` | 否  | true | 是否安装基础工具 |

## 使用方式

```bash
./k8s-offline-tool -config config.yaml
```

## 操作系统以及内核版本支持清单
后续持续添加适配其它操作系统及内核

| 操作系统 | 内核版本 |
| -- | --- |
| Ubuntu 24.04 | 6.8.0-90-generic  |
| Fedora Linux 41 | 6.11.4-301.fc41.x86_64 |


## 基础工具列表
- 监控类：htop
- 数据格式化：jq、bash-completion
- 下载类：dnf-plugins-core（apt-transport-https）、wget 、curl
- 网络类：net-tools、iproute-tc（iproute2）、NetworkManager-tui、bridge-utils、bind-utils（bind9-utils）、tcpdump 
- 代码工具：git、make、vim