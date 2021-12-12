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
