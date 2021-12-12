![firecracker-in-docker](diagrams/firecracker-in-docker.png) 

firecracker-in-docker is a proof of concept project to run [Firecracker](https://github.com/firecracker-microvm/firecracker) MicroVMs inside *unprivileged* Docker containers.

The goal is to transform unmodified Docker images into a new image comprising a Linux kernel and root filesystem run in a Firecracker MicroVM launched by the container, such that the resulting container behaves as closely as possible to the way an equivalent traditional Docker container using the same source image would.

## Motivation
Containers are awesome, but there are some use cases, like [multitenancy](https://en.wikipedia.org/wiki/Multitenancy), and requirements to handle potentially malicious workloads or data, where the additional isolation provided by hardware virtualisation, [hypervisors](https://en.wikipedia.org/wiki/Hypervisor), and guest kernels might be beneficial.

To be clear, there is no *fundamental* truth that Virtual Machines (VMs) are more secure than containers and indeed there is quite the [cargo cult](https://en.wikipedia.org/wiki/Cargo_cult_science) around that assumed hypothesis. The reality is more subtle and relates to the available attack surface, and a well-secured container with tightly bounded [seccomp](https://en.wikipedia.org/wiki/Seccomp) and [mandatory access control](https://en.wikipedia.org/wiki/Mandatory_access_control) profiles can be just as secure as a VM. Indeed, some [IBM research](https://blog.hansenpartnership.com/containers-and-cloud-security/) suggests that well-secured containers could actually be **more** secure than typical hypervisors, as pointed out in [this blog](https://www.zdnet.com/article/which-is-more-secure-containers-or-virtual-machines-the-answer-will-surprise-you/).

All that said it requires a degree of effort and skill to adequately secure a container, whether it be an application specific seccomp profile or building applications on top of a [library OS](https://github.com/Solo5/solo5) to minimise the kernel space attack surface (or [both](https://nabla-containers.github.io/)), and an out-of-the-box VM tends to be more secure than a poorly secured container.

So there *may* be advantages to running *some* workloads on VMs, but conversely the ecosystem around containers is far more ubiquitous. This project aims to "hide" the fact that a VM is being used to host the workload by wrapping the hypervisor, guest kernel, and root filesystem in a container that may be deployed and orchestrated like any other containerised workload.

There are several other projects with a similar goal, most notably [Kata Containers](https://github.com/kata-containers/kata-containers) and [firecracker-containerd](https://github.com/firecracker-microvm/firecracker-containerd). A key difference, however, is that both of those deploy a *custom container runtime*, which isn't always desirable nor in some cases even possible (for example with a managed container hosting service). Requiring a custom container runtime can also introduce hard to resolve dependencies, for example Kata Containers [doesn't work well with recent versions of Docker](https://github.com/kata-containers/runtime/issues/3038) as it has dependencies on the now deprecated devicemapper storage driver and uses containerd shimV2 which is not yet supported by Docker.

There are pros and cons to both approaches. With Kata Containers and firecracker-containerd container deployment is arguably more *transparent*, whereas with firecracker-in-docker the source image requires an explicit  *transformation* step. Conversely, our approach is simpler, has fewer "moving parts", uses a standard container runtime and the run time images are stand-alone and more easily customisable.

With firecracker-in-docker the available guest attack surface is arguably lower than with Kata Containers and firecracker-containerd. Both of those run a guest image that comprises, in addition to the application container image, a container engine and an agent that communicates with the host. Conversely, firecracker-in-docker behaves much more like a Unikernel, where we boot the kernel then init directly to the binary specified by the container ENTRYPOINT+CMD from the image that we have converted to the Firecracker rootfs. If that image is a minimal image built off [scratch](https://docs.docker.com/develop/develop-images/baseimages/#create-a-simple-parent-image-using-scratch) then we are very close indeed to operating like a Unikernel. Moreover, we could customise further to create bespoke kernels, for example for applications that do not require network support we could build a kernel with no network stack and communicate with the guest via virtio-serial.

## Prerequisites
Although a goal of this project is to be able to run [Firecracker](https://github.com/firecracker-microvm/firecracker) MicroVMs inside *unprivileged* Docker containers, to achieve that there are some prerequisites.

The most significant prerequisite is that the container must have access to the /dev/kvm device, for example via `--device=/dev/kvm`. This also implies that virtualisation support is available and KVM is installed on the host.

To check whether a host has virtualisation support available, run:
```
egrep -c '(vmx|svm)' /proc/cpuinfo
```
A result greater than 0 implies that virtualisation is supported. Note that if the intended host is itself a virtual machine then it must support nested virtualisation to run firecracker-in-docker.

To check if virtual technology extensions are enabled in the BIOS, use the `kvm-ok` command, which may need to be installed via:
```
sudo apt install cpu-checker
```
A successful result of running `kvm-ok` should look like:
```
INFO: /dev/kvm exists
KVM acceleration can be used
```
To install the essential KVM packages on a Debian or Ubuntu based system run the following command:
```
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils
```
alternatively, to install a more complete set of QEMU/KVM packages run the following command:
```
$ sudo apt install -y qemu qemu-kvm libvirt-daemon libvirt-clients bridge-utils virt-manager
```
As only members of the kvm and libvirt groups can run Virtual Machines it may be necessary to add those (replacing [username] with the actual username):
```
sudo adduser [username] kvm
sudo adduser [username] libvirt
```
To verify that the virtualisation daemon is running, run:
```
sudo systemctl status libvirtd
```
This may be set to start on boot with the following command:
```
sudo systemctl enable --now libvirtd
```

## Architecture
A firecracker-in-docker container is really just a regular Docker container that may be run in exactly the same way as any other Docker container, that is to say it has no dependencies on any particular container runtime.

From a deployment perspective, the only *unusual* thing about firecracker-in-docker containers is that they require access to /dev/kvm (e.g. via `--device=/dev/kvm`) to be able to launch VMs inside the container.

In addition, in order to establish the network route and port forwarding (inside the container's network namespace) from the container's network interface to the MicroVM guest hosted by the container, firecracker-in-docker containers require access to /dev/net/tun (e.g. via `--device=/dev/net/tun`) and the capabilities CAP_NET_RAW and CAP_NET_ADMIN. Note that CAP_NET_ADMIN is the only *additional* capability required, as CAP_NET_RAW is from the [default Docker capability set](https://docs.docker.com/engine/reference/run/#runtime-privilege-and-linux-capabilities). Aside from those two capabilities all other capabilities may be dropped (though some applications might require CAP_NET_BIND_SERVICE to bind to privileged ports) and firecracker-in-docker containers may be run as arbitrary non-root users.

The conceptual architecture of a firecracker-in-docker container is illustrated below:

![architecture](diagrams/architecture.png) 

At startup the firecracker-in-docker ENTRYPOINT ([firestarter](launcher/resources/firestarter)) performs the following steps (described in more detail in the [launcher](launcher) documentation):

- The MicroVM's root filesystem is resized. With regular containers the container's writable layer will simply grow until the underlying filesystem limits are reached, but a MicroVM requires its own filesystem. The [image-builder](image-builder) deliberately shrinks the MicroVM root filesystem to minimise image size, so we resize it at run-time based on the FC_EPHEMERAL_STORAGE environment variable setting.

- Any environment variables that might have been supplied at run time (e.g. via -e/--env options) are inferred and written to a file in /etc/profile.d on the MicroVM's root filesystem, to be read by init after booting the VM.

- The number of MicroVM virtual CPUs and the VM memory required is inferred from the container's /sys/fs/cgroup information (itself derived from the `--cpus=` and `--memory=` options) or obtained from the FC_VCPU_COUNT and FC_MEM_SIZE environment variables, or given default values if neither of those are set.

- If the source image has its ENTRYPOINT set to /sbin/init then firecracker-in-docker will use that directly. However, if the source image is a more typical Docker image, where ENTRYPOINT/CMD is a regular application, the [image builder](image-builder) will have populated the root filesystem with a simple /sbin/init-entrypoint init script and the kernel init boot parameter is set to use that. In other words, after booting, the MicroVM guest kernel inits the application as PID 1 in the same way as the ENTRYPOINT becomes PID 1 in a regular Docker container.

- The network route and port forwarding from the container’s network interface to the MicroVM guest is established by creating a [tap](https://www.kernel.org/doc/html/v5.12/networking/tuntap.html) device and establishing a NAT as described in the [Firecracker network setup documentation](https://github.com/firecracker-microvm/firecracker/blob/main/docs/network-setup.md#on-the-host). The routing is encapsulated inside the container’s network namespace and [port forwarding](https://www.adamintech.com/how-to-use-iptables-for-port-forwarding/) is used to forward everything from the container's eth0 interface to the guest. This means that applications may use the *container's* network (like a regular container) and the container will transparently forward packets to the guest.

- The VM configuration file is created using variables populated during firestarter setup.

- Firecracker is started using the generated VM configuration. This uses the kernel and root filesystem embedded in the firecracker-in-docker image.
 
## Getting Started
### Build a kernel
To begin working with Firecracker an appropriately configured Linux kernel is required. The best way to obtain a kernel is to follow the instructions in the [kernel-builder](kernel-builder) section of this repository, which provides a Docker based kernel build system.

Alternatively, the [stock v4.14.174 kernel](https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin) linked in the [Firecracker getting started guide](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md#running-firecracker) may be downloaded and copied to [launcher/kernel/vmlinux](launcher/kernel/vmlinux). Using the stock kernel is not recommended however, as it is a little old and doesn't support the `random.trust_cpu=on` kernel option, which can cause startup issues with some applications due to low entropy on VM start up.

### Learn how it works
Once a kernel has been obtained it is recommended, though not essential, to read the [Firecracker rootfs tutorial](firecracker-rootfs-tutorial) to understand the approach used by firecracker-in-docker for converting Docker images into an equivalent Firecracker root filesystem as an unprivileged user.

Similarly, it is advisable to read the documentation in the [launcher](launcher) directory to understand the technical details of how firecracker-in-docker actually launches Firecracker MicroVMs and [image-builder](image-builder) which expands on the rootfs tutorial to provide more depth on how the images are created.

### Try out some examples
The [examples](examples) directory provides some simple *quick start* style examples and is a good place to become acquainted with firecracker-in-docker.

### Transform your own images
After trying out the [examples](examples), its time to start exploring further with your own images.

Remember that firecracker-in-docker launches the application in a MicroVM, so some images might not work as expected, others might require modification to work well running in a VM, and others might not work at all.

In general, images that require additional devices, shared IPC, or mounted storage are the ones most likely to be problematic getting to work with firecracker-in-docker and it is advisable to understand the limitations described below.

## Limitations
It is important to note from the [Firecracker Charter](https://github.com/firecracker-microvm/firecracker/blob/main/CHARTER.md) that the **primary** use case for Firecracker is to provide secure, multi-tenant execution of workloads, which means that many of the *limitations* described below are in fact actually **features**.

### Volume mounts are not supported
[Host filesystem sharing](https://github.com/firecracker-microvm/firecracker/issues/1180) is not currently supported by Firecracker (and might never be) as the [attack surface implications are large](https://github.com/firecracker-microvm/firecracker/pull/1351#issuecomment-667085798).

- TODO - It is possible to provide *limited* support in the form of passing read-only "snapshots" of mounted volumes to the guest filesystem at startup. This should be relatively simple to implement using the same approach currently used to pass run-time environment variables and is likely a good approach for things like secrets and configuration.
- TODO -Using something like [NFS](https://en.wikipedia.org/wiki/Network_File_System) should be possible as a work-around. This is basically what AWS does with [Fargate](https://aws.amazon.com/fargate/) where Fargate [added support for EFS](https://aws.amazon.com/blogs/aws/new-aws-fargate-for-amazon-eks-now-supports-amazon-efs/), which is basically a managed NFS service.

### Ephemeral storage may require configuration
With regular Docker containers the container's writable layer will simply grow until the underlying filesystem limits are reached, but a MicroVM requires its own filesystem which needs to be set to a specific size.

At build time the [image-builder](image-builder), by default, deliberately shrinks the MicroVM root filesystem to contain only those blocks that are actually used in order to minimise image size. At run-time we therefore resize the root filesystem to a more useful size based on the FC_EPHEMERAL_STORAGE environment variable setting.

If FC_EPHEMERAL_STORAGE is unset, the default is to resize the root filesystem to double its minimised size. If it is set to a value greater than zero then the root filesystem will be resized to the specified size, as interpreted by [resize2fs](https://man7.org/linux/man-pages/man8/resize2fs.8.html). If no units are specified, the units of the size parameter shall be the file system blocksize of the file system. Optionally, the size parameter may be suffixed by one of the following units designators: 'K', 'M', 'G', 'T' (either upper-case or lower-case) or 's' for power-of-two kilobytes, megabytes, gigabytes, terabytes or 512 byte sectors respectively. If set to zero  then the root filesystem will not be resized.

### Limited device support
Firecracker, by design, supports only a limited set of devices.

- Nested [i8259](https://wiki.osdev.org/8259_PIC) Programmable Interrupt Controller (PIC) chips and an [IOAPIC](https://wiki.osdev.org/IOAPIC) (emulated in-kernel by KVM) - disabled in firecracker-in-docker by the `noapic` kernel boot parameter.
- [i8254](https://wiki.osdev.org/Programmable_Interval_Timer) Programmable Interval Timer (emulated in-kernel by KVM)
- [i8042](https://wiki.osdev.org/%228042%22_PS/2_Controller) PS/2 Keyboard and Mouse Controller (emulated by Firecracker only as a minimal ctrl_alt_del handler in [devices/src/legacy/i8042.rs](https://github.com/firecracker-microvm/firecracker/blob/main/src/devices/src/legacy/i8042.rs))
- Serial console (emulated by Firecracker in [devices/src/legacy/serial.rs](https://github.com/firecracker-microvm/firecracker/blob/main/src/devices/src/legacy/serial.rs))
- VirtIO Block (emulated by Firecracker in [devices/src/virtio/block](https://github.com/firecracker-microvm/firecracker/tree/main/src/devices/src/virtio/block))
- VirtIO Net (emulated by Firecracker in [devices/src/virtio/net.rs](https://github.com/firecracker-microvm/firecracker/tree/main/src/devices/src/virtio/net))
- TODO - It *might* be possible to support some [hardware acceleration](https://blog.cloudkernels.net/posts/vaccel/) in the future, for example with this [vAccel-virtio](https://blog.cloudkernels.net/posts/vaccel_v2/) approach from [vAccel](https://vaccel.org/), though this is currently a work in progress.

### Limited IPC sharing
Containers that rely on sharing [Inter-Process Communication](https://docs.docker.com/engine/reference/run/#ipc-settings---ipc) (IPC) primitives, Unix domain sockets, pipes, shared memory,  etc. are unlikely to *transparently* work as expected, as those all rely on in-kernel primitives.

- TODO - It may well be possible to work-around some of these limitations by making use of Firecracker's support for [virtio-vsock](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md). This is an efficient host-guest communication mechanism exposed as a Unix domain socket on the host-end, but it is likely to require additional proxying on both host and guest and so is not a fully transparent solution and needs some further investigation.
- TODO - virtio-vsock might also be a good way to support highly locked-down guests, where we might wish to fully block guest networking by compiling out network support in the guest kernel.
	
### Running interactively is not transparent
With regular Docker containers it is relatively common to run interactively in the [foreground](https://docs.docker.com/engine/reference/run/), keeping STDIN open and allocating a [pseudo-tty](https://en.wikipedia.org/wiki/Pseudoterminal), e.g. by using `docker run -it`.

Whilst it is possible to run firecracker-in-docker containers using `-it`, this does not transparently propagate to the guest. This may be seen most clearly when launching a simple bash image, which is likely to report:
```
bash: cannot set terminal process group (-1): Inappropriate ioctl for device
bash: no job control in this shell
```
Running the `tty` command in that shell reports:
```
not a tty
```

As a [workaround](https://itectec.com/unixlinux/linux-error-trying-to-run-agetty-in-a-runit-based-linux-installation/), running either:
```
setsid /sbin/agetty --autologin root ttyS0
```
or
```
exec /sbin/agetty --autologin root ttyS0
```
will launch an interactive subshell with an allocated TTY.

### There is no (obvious) way to transparently docker exec
With regular containers it is possible to run a command in a running container using `docker exec`, however that works by entering the namespaces and cgroups of the container which are in-kernel constructs.

Workarounds include including running an ssh server in the guest, or something more sophisticated like a guest "agent", or even a container runtime on the guest. These would, however, increase complexity and the potential available guest attack surface, whereas the primary goals of firecracker-in-docker are simplicity and security, preferring the lowest possible guest surface needed to actually run the application.

### Images are not extendable
With Docker, it is common to create base images and then extend those. However, because firecracker-in-docker images package the application root filesystem into an opaque ext4.rootfs file it is not possible to extend them.

It should be remembered, however, that the that the **primary** use case for Firecracker is to provide secure, multi-tenant execution of workloads. In general "traditional" distribution base images (even minimal ones like Alpine) include far more binaries and libraries than are *actually* required to run the application and when images are extended the additional package dependencies tend to exacerbate that situation further.

Therefore, rather than being seem as a limitation, the fact that firecracker-in-docker basically creates a "flattened" filesystem is actually an advantage from a security perspective. The best way to build images is really to use multi-stage builds, preferably with the final stage being built using the [scratch](https://docs.docker.com/develop/develop-images/baseimages/#create-a-simple-parent-image-using-scratch) base image, and preferably following the [microcontainers](https://blogs.oracle.com/developers/post/the-microcontainer-manifesto-and-the-right-tool-for-the-job) philosophy. By creating an image that includes only the application binary and the shared libraries it *actually* requires one can significantly reduce the potential user space attack surface available in the Firecracker guest VM.

### MicroVM termination is currently not very clean
Currently, signals are not particularly well handled nor propagated to the guest VM. In simple terms this means that, when a firecracker-in-docker container is stopped, the MicroVM is killed somewhat uncleanly. This is basically the equivalent of stopping everything with SIGKILL and can have some implications for applications.

One example is where a client application has connected to a RabbitMQ queue requesting exclusive access. In that scenario the broker prevents other applications connecting to the same queue, however, if the client is killed uncleanly the broker won't immediately release its lock. If the application is restarted it is likely to see an error like: 
```
ACCESS_REFUSED - queue 'xxx' in vhost '/' in exclusive use
```
Other applications that rely on clean termination of clients to release server locks may have similar issues and unfortunately it is not yet clear how to resolve this problem.
