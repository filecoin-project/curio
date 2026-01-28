---
description: This guide will show how to build, install and update Curio binaries
---

# Installation

## Debian package installation

Curio packages are available to be installed directly on Ubuntu / Debian systems.

{% hint style="danger" %}
Debain packages are only available for mainnet right now. For any other network like calibration network or devnet, binaries must be built from source.
{% endhint %}

1.  Install prerequisites

    ```shell
    sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git jq pkg-config curl clang build-essential hwloc libhwloc-dev wget libarchive-dev libgmp-dev libconfig++-dev -y && sudo apt upgrade -y
    ```
2.  Enable Curio package repo

    ```bash
    sudo wget -O /usr/share/keyrings/curiostorage-archive-keyring.gpg https://filecoin-project.github.io/apt/KEY.gpg

    echo "deb [signed-by=/usr/share/keyrings/curiostorage-archive-keyring.gpg] https://filecoin-project.github.io/apt stable main" | sudo tee /etc/apt/sources.list.d/curiostorage.list

    sudo apt update
    ```
3.  Install Curio binaries based on your GPU.

    For NVIDIA GPUs:

    ```bash
    sudo apt install curio-cuda
    ```

    For OpenCL GPUs:

    ```bash
    sudo apt install curio-opencl
    ```

## Linux Build from source

You can build the Curio executables from source by following these steps.

### Software dependencies

You will need the following software installed to install and run Curio.

#### System-specific

Building Curio requires some system dependencies, usually provided by your distribution.

{% hint style="warning" %}
**Note (Supraseal now builds by default on Linux):** Curio’s Linux build chain now always builds `extern/supraseal` as part of the normal `make deps/build` flow. This means **building Curio on Linux requires Supraseal build dependencies**, including:

- CUDA Toolkit **12.x or newer** (needs `nvcc`, even if you won’t run Supraseal at runtime)
- GCC **13** toolchain (`gcc-13` / `g++-13`)
- Python venv tooling (`python3-venv`) and common build tools (`autoconf`, `automake`, `libtool`, `nasm`, `xxd`, etc.)

If you’re troubleshooting whether your machine can run the **fast TreeR** path used by SnapDeals, run:

```bash
curio test supra system-info
```
{% endhint %}

Arch:

```shell
sudo pacman -Syu opencl-icd-loader gcc git jq pkg-config opencl-headers hwloc libarchive nasm xxd python python-pip python-virtualenv
# For Supraseal builds (SnapDeals fast TreeR / batch sealing toolchain):
sudo pacman -Syu cuda
# GCC 13 may be required depending on your supraseal version; install via your distro/AUR as appropriate.
```

Ubuntu 24.04 / Debian:

```shell
sudo apt install -y \
  mesa-opencl-icd ocl-icd-opencl-dev \
  gcc-13 g++-13 \
  git jq pkg-config curl clang build-essential hwloc libhwloc-dev wget \
  python3 python3-dev python3-pip python3-venv \
  autoconf automake libtool \
  xxd nasm \
  libarchive-dev libssl-dev uuid-dev libfuse3-dev \
  libnuma-dev libaio-dev libkeyutils-dev libncurses-dev \
  libgmp-dev libconfig++-dev \
  && sudo apt upgrade -y

# CUDA Toolkit (Supraseal build requirement; needs nvcc)
# Install via NVIDIA’s CUDA repository packages for your distro, or use `cuda-toolkit` packages if available.
```

Fedora:

```shell
sudo dnf -y install gcc make git jq pkgconfig mesa-libOpenCL mesa-libOpenCL-devel opencl-headers ocl-icd ocl-icd-devel clang llvm wget hwloc hwloc-devel libarchive-devel
```

OpenSUSE:

```shell
sudo zypper in gcc git jq make libOpenCL1 opencl-headers ocl-icd-devel clang llvm hwloc libarchive-devel && sudo ln -s /usr/lib64/libOpenCL.so.1 /usr/lib64/libOpenCL.so
```

Amazon Linux 2:

```shell
sudo yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm; sudo yum install -y git gcc jq pkgconfig clang llvm mesa-libGL-devel opencl-headers ocl-icd ocl-icd-devel hwloc-devel libarchive-devel
```

### Rustup

Curio needs [rustup](https://rustup.rs/). The easiest way to install it is:

```shell
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Go

To build Curio, you need a working installation of [Go](https://golang.org/dl/): It needs to be at-least [the version specified here](../../GO_VERSION_MIN/).

Example of an OLD version's CLI download:

```shell
wget -c https://golang.org/dl/go1.23.6.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
```

{% hint style="info" %}
You’ll need to add `/usr/local/go/bin` to your path. For most Linux distributions you can run something like:

```shell
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
```

See the [official Golang installation instructions](https://golang.org/doc/install) if you get stuck.
{% endhint %}

### System Configuration

Before you proceed with the installation, you should increase the UDP buffer. You can do this by running the following commands:

```shell
sudo sysctl -w net.core.rmem_max=2097152
sudo sysctl -w net.core.rmem_default=2097152
```

To persist the UDP buffer size across the reboot, update the `/etc/sysctl.conf` file.

```bash
echo 'net.core.rmem_max=2097152' | sudo tee -a /etc/sysctl.conf
echo 'net.core.rmem_default=2097152' | sudo tee -a /etc/sysctl.conf
```

### Build and install Curio

Once all the dependencies are installed, you can build and install Curio.

1.  Clone the repository:\


    ```bash
    git clone https://github.com/filecoin-project/curio.git
    cd curio/
    ```
2.  Switch to the latest stable release branch:\


    ```bash
    git checkout <release version>
    ```
3.  Enable the use of SHA extensions by adding these two environment variables:



    <pre class="language-bash"><code class="lang-bash">export RUSTFLAGS="-C target-cpu=native -g"
    export FFI_BUILD_FROM_SOURCE=1

    <strong>echo 'export RUSTFLAGS="-C target-cpu=native -g"' >> ~/.bashrc
    </strong>echo 'export FFI_BUILD_FROM_SOURCE=1' >> ~/.bashrc
    source ~/.bashrc
    </code></pre>
4.  If you are using a **Nvidia GPU**, please set the below environment variables.\


    ```bash
    export FFI_USE_CUDA=1
    export FFI_USE_CUDA_SUPRASEAL=1

    echo 'export FFI_USE_CUDA=1' >> ~/.bashrc
    echo 'export FFI_USE_CUDA_SUPRASEAL=1' >> ~/.bashrc
    source ~/.bashrc
    ```

    {% hint style="warning" %}
    On Linux, the Curio build **requires CUDA by default** and will fail if `nvcc` is not found in your PATH. This ensures you don't accidentally build without proper GPU support.

    If you want to use OpenCL instead of CUDA (e.g., for AMD GPUs or systems without CUDA), build with:
    ```bash
    make FFI_USE_OPENCL=1 build
    ```
    {% endhint %}

5.  Curio is compiled to operate on a single network. Choose the network you want to join, then run the corresponding command to build the Curio node:\


    ```shell
    # For Mainnet:
    make clean build

    # For Calibration Testnet:
    make clean calibnet
    ```


6.  Install Curio. This will put `curio` in `/usr/local/bin`. `curio` will use the `$HOME/.curio` folder by default.



    ```shell
    sudo make install
    ```
7. Run `curio --version`

```md
curio version 1.24.5+mainnet+git_214226e7_2025-02-19T17:02:54+04:00
# or
curio version 1.24.5+calibnet+git_214226e7_2025-02-19T17:02:54+04:00
```

You should now have Curio installed. You can now [finish setting up the Curio node](setup.md).

## MacOS Build from source

You can build the Curio executables from source by following these steps.

### Software dependencies

You must have XCode and Homebrew installed to build Curio from source.

#### **XCode Command Line Tools**

Curio requires that X-Code CLI tools be installed before building the Curio binaries.

Check if you already have the XCode Command Line Tools installed via the CLI, run:

```shell
xcode-select -p
```

This should output something like:

```plaintext
/Library/Developer/CommandLineTools
```

If this command returns a path, then you have Xcode already installed! You can [move on to installing dependencies with Homebrew](installation.md#homebrew). If the above command doesn’t return a path, install Xcode:

```shell
xcode-select --install
```

Next up is installing Curio’s dependencies using Homebrew.

### **Homebrew**

We recommend that macOS users use [Homebrew](https://brew.sh/) to install each of the necessary packages.

Use the command `brew install` to install the following packages:

```shell
brew install jq pkg-config hwloc coreutils
brew install go@1.24
```

Next up is cloning the Lotus repository and building the executables.

### **Rust**

Rustup is an installer for the systems programming language Rust. Run the installer and follow the onscreen prompts. The default installation option should be chosen unless you are familiar with customisation:

```shell
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Build and install Curio

The installation instructions are different depending on which CPU is in your Mac:

* [ARM-based CPUs (M1, M2, M3)](installation.md#arm-based-cpus)
* [Intel CPUs](installation.md#intel-cpus)

#### **Arm based CPUs**

1.  Clone the repository:

    ```shell
    git clone https://github.com/filecoin-project/curio.git
    cd curio/
    ```
2.  Switch to the latest stable release branch:

    ```shell
    git checkout <release version>
    ```
3.  Create the necessary environment variables to allow Curio to run on Arm architecture:

    ```shell
    export LIBRARY_PATH=/opt/homebrew/lib
    export FFI_BUILD_FROM_SOURCE=1
    export PATH="$(brew --prefix coreutils)/libexec/gnubin:/usr/local/bin:$PATH"
    ```
4.  Build the `curio` binary:

    ```shell
    make clean curio
    ```
5.  Run the final `make` command to move this `curio` executable to `/usr/local/bin`. This allows you to run `curio` from any directory.

    ```shell
    sudo make install
    ```
6.  Run `curio --version`

    ```md
    curio version 1.24.5+mainnet+git_214226e7_2025-02-19T17:02:54+04:00
    # or
    curio version 1.24.5+calibnet+git_214226e7_2025-02-19T17:02:54+04:00
    ```
7. You should now have Curio installed. You can now [finish setting up the Curio node](setup.md).

#### **Intel CPUs**

❗These instructions are for installing Curio on an Intel Mac. If you have an Arm-based CPU, use the [Arm-based CPU instructions ↑](installation.md#arm-based-cpus)

1.  Clone the repository:

    ```shell
    git clone https://github.com/filecoin-project/curio.git
    cd curio/
    ```
2.  Switch to the latest stable release branch:

    ```shell
    git checkout <release version>
    ```
3.  Build and install Curio:

    ```shell
    make clean curio
    sudo make install
    ```
4.  Run `curio --version`

    ```md
    curio version 1.23.0+mainnet+git_ae625a5_2024-08-21T15:21:45+04:00
    # or
    curio version 1.23.0+calibnet+git_ae625a5_2024-08-21T15:21:45+04:00
    ```

You can now [finish setting up the Curio node](setup.md).
