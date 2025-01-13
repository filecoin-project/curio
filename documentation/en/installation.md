---
description: This guide will show how to build, install and update Curio binaries
---

# Installation

## Debian package installation

Curio packages are available to be installed directly on Ubuntu / Debian systems.

{% hint style="danger" %}
Debain packages are only available for mainnet right now. For any other network like calibration network or devnet, binaries must be built from source.
{% endhint %}

1.  Install prerequisites\


    ```shell
    sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget libarchive-dev -y && sudo apt upgrade -y
    ```
2.  Enable Curio package repo\


    ```bash
    sudo wget -O /usr/share/keyrings/curiostorage-archive-keyring.gpg https://filecoin-project.github.io/apt/KEY.gpg

    echo "deb [signed-by=/usr/share/keyrings/curiostorage-archive-keyring.gpg] https://filecoin-project.github.io/apt stable main" | sudo tee /etc/apt/sources.list.d/curiostorage.list

    sudo apt update
    ```
3.  Install Curio binaries based on your GPU.\
    \
    For NVIDIA GPUs:\


    ```bash
    sudo apt install curio-cuda
    ```

    \
    For OpenCL GPUs:



    ```bash
    sudo apt install curio-opencl
    ```

## Linux Build from source&#x20;

You can build the Curio executables from source by following these steps.

### Software dependencies&#x20;

You will need the following software installed to install and run Curio.

#### System-specific&#x20;

Building Curio requires some system dependencies, usually provided by your distribution.

Arch:

```shell
sudo pacman -Syu opencl-icd-loader gcc git bzr jq pkg-config opencl-icd-loader opencl-headers opencl-nvidia hwloc libarchive
```

Ubuntu/Debian:

```shell
sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget libarchive-dev -y && sudo apt upgrade -y
```

Fedora:

```shell
sudo dnf -y install gcc make git bzr jq pkgconfig mesa-libOpenCL mesa-libOpenCL-devel opencl-headers ocl-icd ocl-icd-devel clang llvm wget hwloc hwloc-devel libarchive-devel
```

OpenSUSE:

```shell
sudo zypper in gcc git jq make libOpenCL1 opencl-headers ocl-icd-devel clang llvm hwloc libarchive-devel && sudo ln -s /usr/lib64/libOpenCL.so.1 /usr/lib64/libOpenCL.so
```

Amazon Linux 2:

```shell
sudo yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm; sudo yum install -y git gcc bzr jq pkgconfig clang llvm mesa-libGL-devel opencl-headers ocl-icd ocl-icd-devel hwloc-devel libarchive-devel
```

### Rustup&#x20;

Curio needs [rustup](https://rustup.rs/). The easiest way to install it is:

```shell
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Go&#x20;

To build Curio, you need a working installation of [Go](https://golang.org/dl/):
It needs to be at-least [the version specified here](https://github.com/filecoin-project/curio/blob/main/GO_VERSION_MIN).

Example of an OLD version's CLI download:

```shell
wget -c https://golang.org/dl/go1.22.3.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
```

{% hint style="info" %}
You’ll need to add `/usr/local/go/bin` to your path. For most Linux distributions you can run something like:

```shell
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
```

See the [official Golang installation instructions](https://golang.org/doc/install) if you get stuck.
{% endhint %}

### System Configuration&#x20;

Before you proceed with the installation, you should increase the UDP buffer. You can do this by running the following commands:

```shell
sudo sysctl -w net.core.rmem_max=2097152
sudo sysctl -w net.core.rmem_default=2097152
```

### Build and install Curio&#x20;

Once all the dependencies are installed, you can build and install Curio.

1. Clone the repository:

```shell
git clone https://github.com/filecoin-project/curio.git
cd curio/
```

2. Switch to the latest stable release branch:

```shell
git checkout <release version>
```

3. Depending on your CPU model, you will want to export additional environment variables:
   1.  If you have **an AMD Zen or Intel Ice Lake CPU (or later)**, enable the use of SHA extensions by adding these two environment variables:\


       ```shell
       export RUSTFLAGS="-C target-cpu=native -g"
       export FFI_BUILD_FROM_SOURCE=1
       ```

       \
       See the [Native Filecoin FFI section](https://lotus.filecoin.io/storage-providers/curio/install/#native-filecoin-ffi) for more details about this process.
   2.  Some older Intel and AMD processors without the ADX instruction support may panic with illegal instruction errors. To solve this, add the `CGO_CFLAGS` environment variable:\


       ```shell
       export CGO_CFLAGS_ALLOW="-D__BLST_PORTABLE__"
       export CGO_CFLAGS="-D__BLST_PORTABLE__"
       ```


   3.  By default, a ‘multicore-sdr’ option is used in the proofs library. This feature is also used in FFI unless explicitly disabled. To disable building with the ‘multicore-sdr’ dependency, set `FFI_USE_MULTICORE_SDR` to `0`:\


       ```shell
       export FFI_USE_MULTICORE_SDR=0
       ```
4. Build and install Curio:\
   Curio is compiled to operate on a single network.\
   Choose the network you want to join, then run the corresponding command to build the Curio node:

```shell
# For Mainnet:
make clean build

# For Calibration Testnet:
make clean calibnet
```

Install Curio:

```shell
sudo make install
```

This will put `curio` in `/usr/local/bin`. `curio` will use the `$HOME/.curio` folder by default.

Run `curio --version`

```md
curio version 1.23.0+mainnet+git_ae625a5_2024-08-21T15:21:45+04:00
# or
curio version 1.23.0+calibnet+git_ae625a5_2024-08-21T15:21:45+04:00
```

1. You should now have Curio installed. You can now [finish setting up the Curio node](https://lotus.filecoin.io/storage-providers/curio/setup/).

### Native Filecoin FFI&#x20;

Some newer CPU architectures like AMD’s Zen and Intel’s Ice Lake have support for SHA extensions. Having these extensions enabled significantly speeds up your Curio node. To make full use of your processor’s capabilities, make sure you set the following variables **before building from source**:

```shell
export RUSTFLAGS="-C target-cpu=native -g"
export FFI_BUILD_FROM_SOURCE=1
```

This method of building does not produce portable binaries. Make sure you run the binary on the same computer as you built it.

## MacOS Build from source&#x20;

You can build the Curio executables from source by following these steps.

### Software dependencies&#x20;

You must have XCode and Homebrew installed to build Curio from source.

#### **XCode Command Line Tools**&#x20;

Curio requires that X-Code CLI tools be installed before building the Curio binaries.

Check if you already have the XCode Command Line Tools installed via the CLI, run:

```shell
xcode-select -p
```

This should output something like:

```plaintext
/Library/Developer/CommandLineTools
```

If this command returns a path, then you have Xcode already installed! You can [move on to installing dependencies with Homebrew](https://lotus.filecoin.io/storage-providers/curio/install/#homebrew). If the above command doesn’t return a path, install Xcode:

```shell
xcode-select --install
```

Next up is installing Curio’s dependencies using Homebrew.

### **Homebrew**&#x20;

We recommend that macOS users use [Homebrew](https://brew.sh/) to install each of the necessary packages.

Use the command `brew install` to install the following packages:

```shell
brew install go bzr jq pkg-config hwloc coreutils
```

Next up is cloning the Lotus repository and building the executables.

### **Rust**&#x20;

Rustup is an installer for the systems programming language Rust. Run the installer and follow the onscreen prompts. The default installation option should be chosen unless you are familiar with customisation:

```shell
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

### Build and install Curio&#x20;

The installation instructions are different depending on which CPU is in your Mac:

* [ARM-based CPUs (M1, M2, M3)](installation.md#arm-based-cpus)
* [Intel CPUs](installation.md#intel-cpus)

#### **Arm based CPUs**&#x20;

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
    curio version 1.23.0+mainnet+git_ae625a5_2024-08-21T15:21:45+04:00
    # or
    curio version 1.23.0+calibnet+git_ae625a5_2024-08-21T15:21:45+04:00
    ```
7. You should now have Curio installed. You can now [set up a new Curio cluster or migrating from Lotus-Miner](https://lotus.filecoin.io/storage-providers/curio/setup/).

#### **Intel CPUs**&#x20;

❗These instructions are for installing Curio on an Intel Mac. If you have an Arm-based CPU, use the [Arm-based CPU instructions ↑](https://lotus.filecoin.io/storage-providers/curio/install/#arm-based-cpus)

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

    You can now [finish setting up the Curio node](https://lotus.filecoin.io/storage-providers/curio/setup/).
