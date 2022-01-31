# examples
This examples directory provides some simple *quick start* style examples and is a good place to become acquainted with firecracker-in-docker.

## Introduction
N.B. a working Firecracker kernel, e.g. as built by [kernel-builder](../kernel-builder), is required before using image-builder, which is used throughout these examples.

To use image-builder properly, either update the PATH environment variable to include the [image-builder](../image-builder) directory, or create a symlink to the image-builder executable from one of the directories already on PATH.

The basic usage is:
```
image-builder <name>
```
That will build a root filesystem from the specified source image `<name>` and create a `firecracker-<name>` directory containing the Dockerfile, kernel, root filesystem etc. The generated Dockerfile may be used to create a `firecracker-<name>`image in the usual way.

## hello-world
For our first example we will use the hello-world image mentioned in the [Docker installation documentation](https://docs.docker.com/engine/install/ubuntu/) that is used to check if the Docker installation was successful:
```
image-builder hello-world
```
This will directly pull the image layers from DockerHub (without requiring Docker), unpack the layers, then generate a root filesystem. It will then create a firecracker-hello-world directory containing the standalone Dockerfile, kernel, root filesystem etc.

if we:
```
cd firecracker-hello-world
```
then:
```
docker build -t firecracker-hello-world .
```
that will hopefully result in our firecracker-hello-world image being created.

To run:
```
./firecracker-hello-world
```
Instead of generating a standalone Dockerfile, we could instead build a firecracker-in-docker base image by using the [Dockerfile](../launcher/Dockerfile) in the [launcher](../launcher) directory.

If we delete the generated firecracker-hello-world directory created by the previous example and now do:
```
image-builder -b hello-world
```
That will again create a generated firecracker-hello-world directory, but this time the contents will be simpler as we now rely on the base image to do the heavy lifting.

If a name other than firecracker-in-docker is required for the base image, simply use any desired name when building the base image and provide that to the `-b` option, e.g.
```
image-builder -b <base-image-name> hello-world
```
By default, the image-builder will attempt to shrink the root filesystem to its minimum size, which means that the final built firecracker-in-docker images will be as small as possible. By default the firestarter ENTRYPOINT of the container will expand the root filesystem on container startup.

It is possible to specify a preferred image size using the `-s` option:
```
image-builder -b -s 300M hello-world
```
which will create a root filesystem of 300MiB.

To prevent firestarter from attempting to resize a root filesystem that is already of the required size, the container should be started with the FC_EPHEMERAL_STORAGE environment variable set to zero:
```
-e FC_EPHEMERAL_STORAGE=0
```

## focal-demo
For this we will start with the [Dockerfile-focal-demo](Dockerfile-focal-demo) Dockerfile in this examples directory, which we first have to build into the source image in the usual way, e.g.:
```
docker build -t focal-demo -f ./Dockerfile-focal-demo .
```
Because we've just built the image and it is stored locally we can use [docker save](https://docs.docker.com/engine/reference/commandline/save/), which produces a tarred repository containing all parent layers and other image metadata.
```
docker save focal-demo > focal-demo.tar
```
We can then use the tarred source image with image-builder:
```
image-builder focal-demo.tar
```
This will read the tarred image, unpack the layers, then generate a root filesystem. It will then create a firecracker-focal-demo directory containing the standalone Dockerfile, kernel, root filesystem etc.

if we:
```
cd firecracker-focal-demo
```
then:
```
docker build -t firecracker-focal-demo .
```
that will hopefully result in our firecracker-focal-demo image being created, which may then be run as follows:
```
./firecracker-focal-demo
```
This example is slightly more interesting than the previous hello-world, giving us a shell on a Firecracker MicroVM. Note that the hostname of the MicroVM has been set to the container's hostname plus a `-firecracker`suffix, though in general we shouldn't need to care about that as all networking goes via the container hosting the MicroVM and is transparently port forwarded.

As mentioned in the [Limitations](https://github.com/fadams/firecracker-in-docker#limitations) section, [running interactively is not transparent](https://github.com/fadams/firecracker-in-docker#running-interactively-is-not-transparent). That is to say using the `-it` flag of `docker run` does not transparently propagate to the guest. This may be seen in this example where the following message is reported on startup:
```
bash: cannot set terminal process group (-1): Inappropriate ioctl for device
bash: no job control in this shell
```
and running the `tty` command in the shell reports:
```
not a tty
```
To attach a TTY and enable job control the following command may be run:
```
exec /sbin/agetty --autologin root ttyS0
```
If we run:
```
ping google.com
```
We should see replies, assuming that networking is available on the host and that ICMP hasn't been blocked by a hosting provider or corporate VPN.
