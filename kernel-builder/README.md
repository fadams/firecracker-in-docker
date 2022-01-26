# kernel-builder
This directory provides a Docker container and launch script to build a Linux kernel compatible with [Firecracker](https://github.com/firecracker-microvm/firecracker).

## Background
The Firecracker documentation provides some good guidance on [creating a kernel image](https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md#creating-a-kernel-image), though there are some potential surprises for the unwary.

The **most important** thing to be aware of when following that guide is that in section 3. it says 

>You will need to configure your Linux build. You can start from our recommended [x86 config](https://github.com/firecracker-microvm/firecracker/blob/main/resources/microvm-kernel-x86_64.config) (or [aarch64 config](https://github.com/firecracker-microvm/firecracker/blob/main/resources/microvm-kernel-arm64.config)) by copying it to .config (under the Linux sources dir)

Unfortunately, however, the config linked in the documentation is for kernel v4.14.174, but the compilation instructions actually reference v4.20, and moreover we *actually* want to build a more recent kernel.

Although this is a little inconsistent and confusing, starting from the *recommended config* described above is really important even if a different kernel version is required. This is especially true if unfamiliar with building kernels, because simply using:
```
make menuconfig
```
with no initial config will generate a (very much larger) kernel that fails to boot correctly, with a message similar to:
```
[   13.705550] VFS: Cannot open root device "vda" or unknown-block(0,0): error -6
[   13.706902] Please append a correct "root=" boot option; here are the available partitions:
[   13.708279] Kernel panic - not syncing: VFS: Unable to mount root fs on unknown-block(0,0)
```
This is because the default config that is created when not using a "template" config is missing the required Virtio drivers and the Virtio block driver. The default config is also missing tmpfs from the Pseudo filesystems section, which will cause systemd to crash.

In general, therefore, "bootstrapping" the build using the initial recommended config from the Firecracker repo is the path of least pain, even when building later kernels. The [kernel config files used by this repo](resources) were created by taking a very conservative approach, where the original [4.14.174 config](https://github.com/firecracker-microvm/firecracker/blob/main/resources/microvm-kernel-x86_64.config) was used initially, and the later configs were created by simply selecting the default responses during the configuration stage when building the kernels.

## Usage
The kernel-builder [Dockerfile](Dockerfile) is relatively straightforward. In essence it installs the packages required for building the kernel, then adds a build directory and "builder" user, so that the image may be used without bind-mounting a build directory if so desired.

One point to be aware of is that it uses `ubuntu:20.04 `as a base image for consistency with the launcher images in this repo. Unfortunately the default compiler for 20.04 fails to build older kernels (like the v4.20 kernel referred to in the Firecracker documentation). Because we are aiming to build a more modern kernel for firecracker-in-docker that isn't an issue, but it is something to be aware of if an older kernel is required. Simply change the base image to `ubuntu:18.04` if this is an issue.

To build the image:
```
docker build -t firecracker-kernel-builder .
```
The best way to run the container is via the [kernel-builder](kernel-builder) script. The kernel source code can take a while to download, so this script creates a build directory in the current working directory and runs the container as the user running the script, bind mounting the build directory. This means that the kernel source may be downloaded once and the container run using the saved build directory.

Simply running:
```
./kernel-builder
```
will create the build directory then download and build the default kernel version (currently 5.10.93).

Running:
```
./kernel-builder -i
```
will, in addition, launch the interactive kernel configuration menu which will allow a customised kernel configuration based on the default config to be built.

To build a different kernel version pass the version as a command line argument, for example:

```
./kernel-builder -i 5.8
```
In this case it is recommended to also use the `-i` option, but it should be enough to simply use the save button of the kernel configuration menu to save a .config file for the required kernel version.

Depending on available bandwidth and number of available CPUs the build might take some time and when complete the compiled kernel will be available in the build directory e.g.:
```
$PWD/$(id -un)/linux/vmlinux
```

An alternative approach that avoids bind-mounting a build directory is to use:
```
docker run --rm firecracker-kernel-builder -o > vmlinux
cp vmlinux net-admin-launcher/kernel/vmlinux
cp vmlinux user-namespace-launcher/kernel/vmlinux
```
This method builds the kernel entirely in the container's filesystem and exports it via stdout, which is redirected to the vmlinux file. As everything will be lost if anything goes wrong with the download and build, this method of building is not recommended unless it is essential to avoid bind-mounting the build directory.

## Kernel Versions
The reason why 5.10.93 is currently the default kernel version used by this repo is simply because, according to the [kernel releases](https://www.kernel.org/category/releases.html) page, 5.10 is the LTS version with the longest projected OEL date (Dec, 2026).

Another noteworthy point about versions is that versions 5.13 and 5.14 should be avoided (or used with caution). The author noted what *appeared* to be "broken" Garbage Collection in a Python application, leading to eventual OOM. This only manifest itself with kernel versions between 5.13 to 5.14.8 and 5.14.9 and above and 5.15 appear to be OK.
