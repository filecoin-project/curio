---
description: 本指南将展示如何构建、安装和更新Curio二进制文件
---

# Installation
# 安装

## Debian package installation
## Debian包安装

Curio软件包可直接在Ubuntu / Debian系统上安装。

{% hint style="danger" %}
目前Debian包只适用于主网。对于其他网络如校准网络或开发网，必须从源代码构建二进制文件。
{% endhint %}

```
// Start of Selection
1. 安装先决条件

```bash
sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git jq pkg-config curl clang build-essential hwloc libhwloc-dev libarchive-dev libgmp-dev libconfig++-dev wget -y && sudo apt upgrade -y
```

2. 启用Curio软件包仓库

```bash
sudo wget -O /usr/share/keyrings/curiostorage-archive-keyring.gpg https://filecoin-project.github.io/apt/KEY.gpg

echo "deb [signed-by=/usr/share/keyrings/curiostorage-archive-keyring.gpg] https://filecoin-project.github.io/apt stable main" | sudo tee /etc/apt/sources.list.d/curiostorage.list

sudo apt update
```

3. 根据您的GPU安装Curio二进制文件。

对于NVIDIA GPU：

sudo apt install curio-cuda

对于OpenCL GPU：

```bash
sudo apt install curio-opencl
```

## Linux Build from source
## Linux从源代码构建

您可以按照以下步骤从源代码构建Curio可执行文件。

### Software dependencies
### 软件依赖

要安装和运行Curio，您需要安装以下软件。

#### System-specific
#### 系统特定

构建Curio需要一些系统依赖，通常由您的发行版提供。

{% hint style="warning" %}
**注意（Linux 上默认会构建 Supraseal）：** Curio 的 Linux 构建流程现在会在常规的 `make deps/build` 中默认构建 `extern/supraseal`。因此在 Linux 上从源代码构建 Curio 需要 Supraseal 的额外依赖，包括：

- CUDA Toolkit **13.x 或更高版本**（需要 `nvcc`，即使你不打算在运行时使用 Supraseal）
- GCC **13** 工具链（`gcc-13` / `g++-13`）
- Python venv 工具（`python3-venv`）以及常见构建工具（`autoconf`、`automake`、`libtool`、`nasm`、`xxd` 等）

要检查机器是否支持 SnapDeals 的 **快速 TreeR** 路径，可运行：

```bash
curio test supra system-info
```
{% endhint %}

Arch:

```bash
sudo pacman -Syu opencl-icd-loader gcc git bzr jq pkg-config opencl-headers hwloc libarchive nasm xxd python python-pip python-virtualenv
# Supraseal 构建依赖（需要 nvcc）
sudo pacman -Syu cuda
# GCC 13 可能需要通过发行版/AUR 安装（取决于当前 supraseal 版本）
```

Ubuntu/Debian:

```bash
sudo apt install -y \
  mesa-opencl-icd ocl-icd-opencl-dev \
  gcc-13 g++-13 \
  gcc git jq pkg-config curl clang build-essential hwloc libhwloc-dev libarchive-dev wget \
  python3 python3-dev python3-pip python3-venv \
  autoconf automake libtool \
  xxd nasm \
  libssl-dev uuid-dev libfuse3-dev \
  libnuma-dev libaio-dev libkeyutils-dev libncurses-dev \
  libgmp-dev libconfig++-dev \
  && sudo apt upgrade -y

# CUDA Toolkit（Supraseal 构建依赖；需要 nvcc）
```

Fedora:

```bash
sudo dnf -y install gcc make git bzr jq pkgconfig mesa-libOpenCL mesa-libOpenCL-devel opencl-headers ocl-icd ocl-icd-devel clang llvm wget hwloc hwloc-devel libarchive-devel
```

OpenSUSE:

```bash
sudo zypper in gcc git jq make libOpenCL1 opencl-headers ocl-icd-devel clang llvm hwloc libarchive-devel && sudo ln -s /usr/lib64/libOpenCL.so.1 /usr/lib64/libOpenCL.so
```

Amazon Linux 2:

```bash
sudo yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm; sudo yum install -y git gcc bzr jq pkgconfig clang llvm mesa-libGL-devel opencl-headers ocl-icd ocl-icd-devel hwloc-devel libarchive-devel
```

### Rustup

Curio需要[rustup](https://rustup.rs/)。最简单的安装方法是：

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Go

构建 Curio 需要安装 Go（最低版本以仓库根目录的 `GO_VERSION_MIN` 为准）。

示例（当前仓库最小版本为 **1.24.7**）：

```bash
wget -c https://go.dev/dl/go1.24.7.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
```

{% hint style="info" %}
您需要将`/usr/local/go/bin`添加到您的路径中。对于大多数Linux发行版，您可以运行类似以下的命令：


echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc


如果遇到困难，请参阅[官方Golang安装说明](https://golang.org/doc/install)。
{% endhint %}

### System Configuration
### 系统配置

在继续安装之前，您应该增加UDP缓冲区。您可以通过运行以下命令来实现：

```bash
sudo sysctl -w net.core.rmem_max=2097152
sudo sysctl -w net.core.rmem_default=2097152
```

### Build and install Curio
### 构建和安装Curio

一旦所有依赖项都安装完毕，您就可以构建和安装Curio了。

1. 克隆仓库：

```bash
git clone https://github.com/filecoin-project/curio.git
cd curio/
```

2. 切换到最新的稳定版本分支：

```bash
git checkout <release version>
```

3. 根据您的CPU型号，您需要导出额外的环境变量：
    1. 如果您有**AMD Zen或Intel Ice Lake CPU（或更新版本）**，通过添加这两个环境变量来启用SHA扩展的使用：

   ```bash
    export RUSTFLAGS="-C target-cpu=native -g"
    export FFI_BUILD_FROM_SOURCE=1
   ```

    有关此过程的更多详细信息，请参阅[Native Filecoin FFI部分](https://lotus.filecoin.io/storage-providers/curio/install/#native-filecoin-ffi)。

    2. 一些不支持ADX指令的较旧Intel和AMD处理器可能会出现非法指令错误。要解决这个问题，请添加`CGO_CFLAGS`环境变量：

   
    export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
    export CGO_CFLAGS="-D__BLST_PORTABLE__"
   

    3. 默认情况下，proofs库中使用"multicore-sdr"选项。除非明确禁用，否则FFI也会使用此功能。要禁用使用"multicore-sdr"依赖项构建，请将`FFI_USE_MULTICORE_SDR`设置为`0`：

   
    export FFI_USE_MULTICORE_SDR=0
   

4. 构建和安装Curio：
    Curio被编译为在单个网络上运行。
    选择您要加入的网络，然后运行相应的命令来构建Curio节点：


# 对于主网：
make clean build

# 对于校准测试网：
make clean calibnet


安装Curio：

```bash
sudo make install
```

这将把`curio`放在`/usr/local/bin`中。`curio`默认将使用`$HOME/.curio`文件夹。

运行`curio --version`


curio version 1.27.0-dev+mainnet+git.78d9d9baa
# 或
curio version 1.27.0-dev+calibnet+git.78d9d9baa


5. 现在您应该已经安装了Curio。您现在可以[完成Curio节点的设置](https://lotus.filecoin.io/storage-providers/curio/setup/)。

### Native Filecoin FFI
### 原生Filecoin FFI

一些较新的CPU架构，如AMD的Zen和Intel的Ice Lake，支持SHA扩展。启用这些扩展可以显著加速您的Curio节点。要充分利用处理器的功能，请确保在**从源代码构建之前**设置以下变量：

```bash
export RUSTFLAGS="-C target-cpu=native -g"
export FFI_BUILD_FROM_SOURCE=1
```

这种构建方法不会产生可移植的二进制文件。确保您在构建它的同一台计算机上运行二进制文件。

## MacOS Build from source
## MacOS从源代码构建

您可以按照以下步骤从源代码构建Curio可执行文件。

### Software dependencies
### 软件依赖

要从源代码构建Curio，您必须安装XCode和Homebrew。

#### **XCode Command Line Tools**
#### **XCode命令行工具**

在构建Curio二进制文件之前，需要安装X-Code CLI工具。

通过CLI检查是否已安装XCode命令行工具，运行：

```bash
xcode-select -p
```

这应该输出类似以下内容：


/Library/Developer/CommandLineTools


如果此命令返回一个路径，那么您已经安装了Xcode！您可以[继续使用Homebrew安装依赖项](https://lotus.filecoin.io/storage-providers/curio/install/#homebrew)。如果上述命令没有返回路径，请安装Xcode：

```bash
xcode-select --install
```

接下来是使用Homebrew安装Curio的依赖项。

### **Homebrew**
### **Homebrew**

我们建议macOS用户使用[Homebrew](https://brew.sh/)安装每个必要的软件包。

使用命令`brew install`安装以下软件包：

```bash
brew install go bzr jq pkg-config hwloc coreutils
```

接下来是克隆Lotus仓库并构建可执行文件。

### **Rust**

Rustup是系统编程语言Rust的安装程序。运行安装程序并按照屏幕提示操作。除非您熟悉自定义，否则应选择默认安装选项：

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Build and install Curio
### 构建和安装Curio

安装说明因Mac中的CPU类型而异：

* [基于ARM的CPU（M1、M2、M3）](installation.md#arm-based-cpus)
* [Intel CPU](installation.md#intel-cpus)

#### **基于ARM的CPU**

1. 克隆仓库：

```bash
git clone https://github.com/filecoin-project/curio.git
cd curio/
```

2. 切换到最新的稳定版本分支：

```bash
git checkout <release version>
```

3. 创建必要的环境变量以允许Curio在ARM架构上运行：

```bash
export LIBRARY_PATH=/opt/homebrew/lib
export FFI_BUILD_FROM_SOURCE=1
export PATH="$(brew --prefix coreutils)/libexec/gnubin:/usr/local/bin:$PATH"
```

4. 构建`curio`二进制文件：

```bash
make clean curio
```

5. 运行最后的`make`命令将此`curio`可执行文件移动到`/usr/local/bin`。这允许您从任何目录运行`curio`。

```bash
sudo make install
```

6. 运行`curio --version`


curio version 1.27.0-dev+mainnet+git.78d9d9baa
# 或
curio version 1.27.0-dev+calibnet+git.78d9d9baa


7. 现在您应该已经安装了Curio。您现在可以[设置新的Curio集群或从Lotus-Miner迁移](https://lotus.filecoin.io/storage-providers/curio/setup/)。

#### **Intel CPU**

❗这些说明适用于在Intel Mac上安装Curio。如果您有基于ARM的CPU，请使用[基于ARM的CPU说明 ↑](https://lotus.filecoin.io/storage-providers/curio/install/#arm-based-cpus)

1. 克隆仓库：


git clone https://github.com/filecoin-project/curio.git
cd curio/


2. 切换到最新的稳定版本分支：

```bash
git checkout <release version>
```

3. 构建和安装Curio：

```bash
make clean curio
sudo make install
```

4. 运行`curio --version`


curio version 1.27.0-dev+mainnet+git.78d9d9baa
# 或
curio version 1.27.0-dev+calibnet+git.78d9d9baa


现在您可以[完成Curio节点的设置](https://lotus.filecoin.io/storage-providers/curio/setup/)。
