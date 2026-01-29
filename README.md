# k8s-offline-tool

该项目用于在离线或内网环境中安装 Kubernetes，并可在已有集群中部署常用组件（CNI 与存储）。工具通过 SSH 连接目标节点，分发离线资源并执行安装/部署步骤。

## 功能概览

- 离线安装基础组件：linux通用工具包、containerd、runc、nerdctl、kubeadm/kubelet/kubectl。
- 在 master 节点初始化集群，并自动生成 worker 的 join command，若配置中没有master节点，需手动配置worker 节点的 join command。
- 支持私有镜像仓库：同步所需镜像到私有 registry，并在部署时重写镜像地址。前提：程序执行的本地环境以配置能访问该私有仓库。
- 支持预检查模式，检查各安装步骤是否需要执行，不执行安装动作。
- 支持安装模式选择，从零安装并初始化集群还是在已有集群中仅部署k8s组件，组件部署：kube-ovn、multus-cni、local-path-storage（可选）。

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

# 三 Master 高可用配置
ha:
  enabled: true
  virtual_ip: "192.168.1.100/24"


# 节点列表（按顺序进行安装）
nodes:
  - ip: "192.168.1.8"
    password: "root"
    ssh_port: 22
    is_master: true
  - ip: "192.168.1.10"
    password: "root"
    ssh_port: 22
  - ip: "192.168.1.3"
    password: "root"
    ssh_port: 22


# Worker 节点加入集群的命令 (在 is_master: false 的节点上执行)
join_command: "kubeadm join 192.168.1.10:6443 --token <token> --discovery-token-ca-cert-hash <hash>"
# 子Master 节点加入集群的命令 (在 is_master: true,is_primary_master: false 的节点上执行)
master_join_command: ""
```
配置示例见下文

### 字段解释与默认值

#### 顶层字段

| 字段 | 必填 | 默认值    | 说明                                                                                         |
| -- | --- |--------|--------------------------------------------------------------------------------------------|
| `ssh_port` | 否 | `22`   | SSH 端口默认值，可被节点级配置覆盖。                                                                       |
| `user` | 否 | `root` | SSH 用户名。                                                                                   |
| `command_timeout_seconds` | 否 | `600`  | 远程命令执行超时（秒）。                                                                               |
| `install_mode` | 否 | `full` | 安装模式：`full` 为从零安装集群，`addons-only` 为仅部署k8s插件, `install-only` 为仅安装软件，不执行kubeadm init & join及及插件安装 |
| `dry_run` | 否 | `false` | 仅执行预检查，不执行安装动作。                                                                            |
| `versions` | 否 | 见下表    | 离线包版本配置。                                                                                   |
| `addons` | 否 | 见下表    | 组件启用与版本配置。                                                                                 |
| `registry` | 否 | 空      | 私有镜像仓库配置（Harbor），启用后会同步镜像并重写部署文件。                                                          |
| `nodes` | 是 | 见下表    | 节点列表，至少包含一个 `is_master: true` 的节点。                                                         |
| `join_command` | 否 | 空      | worker 加入集群时使用的命令。若未指定，会在 master 初始化后自动生成。                                                 |
| `master_join_command` | 否 | 空      | 子Master 节点加入集群时使用的命令。若未指定，会在 master 节点初始化后自动生成。 |
| `ha` | 否 | 空      | 三 Master 高可用配置。                                                                           |

#### `versions`（支持版本）

| 字段           | 默认值      | 说明            |
|--------------|----------|---------------|
| `docker`         | `29.2.0` | docker-ce 版本  |
| `containerd` | `2.2.1`  | containerd 版本。 |
| `runc`       | `1.3.4`  | runc 版本。      |
| `nerdctl`    | `2.2.1`  | nerdctl 版本。   |
| `k8s`        | `1.35.0` | Kubernetes 版本。 |

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
| `endpoint` | 是  | Harbor 域名（http）。               |
| `port` | 是  | Harbor 端口。                     |
| `ip` | 是  | Harbor 的 IP，用于写入 `/etc/hosts`。 |
| `username` | 是  | Harbor 用户名，用于创建项目和查询镜像。 |
| `password` | 是  | Harbor 密码。 |


#### `nodes`
| 字段 | 必填 | 默认值  | 说明               |
| --- |----|------|------------------|
| `ip` | 是  | -    | 节点 IP            |
| `password` | 是  | -    | 节点登录密码。          |
| `ssh_port` | 否  | 22   | SSH 端口，默认为 `22`。 |
| `is_master` | 否  | false | 是否为 master 节点。   |
| `is_primary_master` | 否  | false | 是否为主 master 节点。  |
| `interface` | 否  | -    | 节点管理网卡名称，ha模式下必填 |


#### `ha`
ha 模式开启时，要求配置3个master节点，其中一个为主 master 节点。

| 字段 | 必填 | 默认值 | 说明 |
| --- |----|-----|----------------|
| `enabled` | 是  | true | 是否启用高可用        |
| `virtual_ip` | 是  | -   | 三主高可用虚拟 IP     |


## 操作系统以及内核版本支持清单
后续持续添加适配其它操作系统及内核

| 操作系统 | 内核版本 |
| -- | --- |
| Ubuntu 24.04 | 6.8.0-90-generic  |
| Fedora Linux 41 | 6.11.4-301.fc41.x86_64 |


## 基础工具列表
程序执行时，会在系统中安装如下附加的基础工具：

* fedora 41 
  * 监控类：htop
  * 下载类：dnf-plugins-core
  * 网络类：iproute-tc、NetworkManager-tui
  * 算力容器运行时工具: nvidia-container-toolkit

* ubuntu 24.04
  * 下载类：apt-transport-https
  * 视图：tree
  * 算力容器运行时工具: nvidia-container-toolkit
  

## 使用方式

```bash
# 编译
go build -o k8s-offline-tool main.go
```

```bash
./k8s-offline-tool -config xxx.yaml
```

## 安装步骤解析


![Installation-steps.png](doc/Installation-steps.png)




## 使用场景

### 场景一：离线环境完整安装 Kubernetes 集群
按顺序部署节点，安装基础工具、容器运行时、配置私有镜像仓库、同步所需镜像、Kubernetes 安装、插件安装，并在第一个 master 节点初始化集群，其他节点加入集群
```bash
root@f1:~# cat config.yaml 
registry:
  endpoint: "jusuan.io"
  port: 8080
  ip: 192.168.1.7
  username: "admin"
  password: "Harbor12345"
nodes:
  - ip: "192.168.1.8"
    password: "root"
    is_master: true
  - ip: "192.168.1.10"
    password: "root"
  - ip: "192.168.1.3"
    password: "root"
addons:
  kube_ovn:
    enabled: true
  multus_cni:
    enabled: true
  local_path_storage:
    enabled: true
    
# 仅执行预检查，正式安装前可先执行预检查模式看看
# dry_run: true 
root@f1:~# ./k8s-offline-tool -config config.yaml
```

### 场景二：在已有集群中部署常用组件
插件可以选择性安装
```bash
root@f1:~# cat config.yaml 
install_mode: "addons-only"
registry:
  endpoint: "jusuan.io"
  port: 8080
  ip: 192.168.1.7
  username: "admin"
  password: "Harbor12345"
nodes:
  - ip: "192.168.1.8"
    password: "root"
addons:
  kube_ovn:
    enabled: true
  multus_cni:
    enabled: false
  local_path_storage:
    enabled: true
root@f1:~# ./k8s-offline-tool -config config.yaml
```

### 场景三：仅安装基础工具和 k8s 组件，不执行 kubeadm init/join 及插件安装
且没有配置私有镜像仓库
```bash
root@f1:~# cat config.yaml 
install_mode: "install-only"
nodes:
  - ip: "192.168.1.8"
    password: "root"
root@f1:~# ./k8s-offline-tool -config config.yaml
```

### 场景四： 将目标work节点加入已存在集群
如有私有镜像仓库，请配置 `registry` 参数
```bash
root@f1:~# cat config.yaml 
install_mode: "full"
nodes:
  - ip: "192.168.1.10"
    password: "root"
  - ip: "192.168.1.3"
    password: "root"
join_command: "xxxx"
root@f1:~# ./k8s-offline-tool -config config.yaml
```


## 📦 运行示例

<p align="center">
  <img src="doc/demo.gif" width="900">

</p>



## 注意事项
私有镜像仓库镜像同步步骤的执行是在本程序运行的本地环境中进行的，确保本地环境可以访问配置的私有仓库。附上配置示例：
### docker
```bash
cat <<EOF > daemon.json
{
  "registry-mirrors": ["https://hdi5v8p1.mirror.aliyuncs.com"],
  "exec-opts": ["native.cgroupdriver=systemd"],
  "insecure-registries" : [ "jusuan.io:8080"]
}
EOF
mv daemon.json /etc/docker/

systemctl enable docker.service
sudo systemctl daemon-reload
systemctl restart docker.service
```

### containerd 2.2版本+
```bash
containerd config default > /etc/containerd/config.toml
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml
sudo sed -i "s|config_path = '/etc/containerd/certs.d:/etc/docker/certs.d'|config_path = '/etc/containerd/certs.d'|g" /etc/containerd/config.toml

sudo mkdir -p /etc/containerd/certs.d/jusuan.io:8080
sudo tee /etc/containerd/certs.d/jusuan.io:8080/hosts.toml >/dev/null <<'EOF'
server = "http://jusuan.io:8080"

[host."http://jusuan.io:8080"]
  capabilities = ["pull", "resolve", "push"]
EOF

systemctl enable containerd.service
sudo systemctl daemon-reload
systemctl restart containerd.service
```



## TODO
* 持续添加适配其它操作系统、架构及内核。
* 持续添加适配其它国产加速卡的驱动、固件、容器运行时工具的检测与安装。
* 持续添加适配其它k8s插件。
* 适需求添加适配k8s版本的升级。





















