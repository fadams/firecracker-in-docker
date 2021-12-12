# firecracker-rootfs-tutorial

This directory contains a tutorial to help users understand the process for building a root filesystem for Firecracker.

## Tutorial
### Introduction
There are many different ways to create a Linux root filesystem, but as the ultimate goal of this repository is to allow us to transform existing Docker images into the equivalent firecracker-in-docker images the focus of this tutorial shall be on explaining a number of different approaches for creating a rootfs image from a Docker image.

To that end, for the purpose of this tutorial we shall use the following example Dockerfile [Dockerfile-focal-demo](Dockerfile-focal-demo), which is a a simple Ubuntu based image that contains a few network applications and the systemd init system, which is unusual in a Docker application, but useful when getting started with Firecracker.
```
FROM ubuntu:20.04

RUN apt-get update && DEBIAN_FRONTEND=noninteractive \
  apt-get install -y --no-install-recommends \
  curl ca-certificates openssh-server \
  iproute2 iptables iputils-ping net-tools \
  # The following is used for Firecracker init
  init udev && \
  # The serial getty service hooks up the login prompt to the
  # kernel console at ttyS0 (where Firecracker connects its
  # serial console). Set to autologin to avoid login prompt.
  mkdir "/etc/systemd/system/serial-getty@ttyS0.service.d/" && \
  echo "[Service]\nExecStart=\nExecStart=-/sbin/agetty --autologin root --keep-baud 115200,38400,9600 %I $TERM\n" > /etc/systemd/system/serial-getty@ttyS0.service.d/autologin.conf && \
  passwd -d root && \
  rm -rf /var/lib/apt/lists/*
```
to build the image use:
```
docker build -t focal-demo -f ./Dockerfile-focal-demo .
```
### Usage
All of the following rootfs creation approaches will use this focal-demo example as a source image and will create a rootfs.ext4 file, which is the Firecracker root filesystem, as an output. To use this root filesystem, it should be copied into the [rootfs](../launcher/rootfs) subdirectory of the [launcher](../launcher) directory. After building the firecracker-in-docker image using the [Dockerfile](../launcher/Dockerfile) in that directory the example may then be run by running the `firecracker` script in the launcher directory.

Note that this approach is primarily intended as a tutorial to help users understand the mechanics of creating a Firecracker root filesystem from a Docker image. For more serious usage, users are pointed to the [image-builder](../image-builder), and the [examples](../examples) directory which uses the image-builder.

### Create a filesystem with mount and docker run
This approach follows a somewhat similar process to that described in the [Firecracker documentation](https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md#manual-build), though rather than bind-mounting the target filesystem here we use `docker export`.

First prepare an empty file of an appropriate size to accomodate the filesystem:
```
dd if=/dev/zero of=rootfs.ext4 bs=1M count=250
```
then actually create the empty filesystem:
```
mkfs.ext4 rootfs.ext4
```

we next create a mountpoint:
```
mkdir -p rootfs
```
and mount our filesystem (noting that mount requires root privileges):
```
sudo mount rootfs.ext4 rootfs
```
next we run a container for our Docker image:
```
docker run -d focal-demo /bin/bash
```
`docker run` will report the container id, which should be used in the following command used to export the container's filesystem to our mountpoint:
```
docker export <container ID returned from previous docker run>| sudo tar xp -C rootfs
```
Finally, we unmount:
```
sudo umount rootfs
```
then tidy up the mountpoint
```
rm -rf rootfs
```
then the container:
```
docker rm <container ID returned from previous docker run>
```

We have combined the required steps into the following [mount-and-docker-run](mount-and-docker-run) shell script.
```
SIZE=250
IMAGE=focal-demo
# To create a Docker style 12 hex character hostname use
# the following:
# HOSTNAME=$(tr -dc 'a-f0-9' < /dev/urandom | head -c12)
HOSTNAME="firecracker"
MOUNTPOINT=rootfs
FILESYSTEM=rootfs.ext4

# Prepare empty file of $SIZE MiB to accomodate filesystem
dd if=/dev/zero of=$FILESYSTEM bs=1M count=$SIZE

# Create an empty ext4 filesystem in the file
mkfs.ext4 $FILESYSTEM

# Create a mountpoint and mount our filesystem
mkdir -p $MOUNTPOINT
sudo mount $FILESYSTEM $MOUNTPOINT

# Run a container for our Docker image, then export the
# container's filesystem to our mountpoint
CONTAINER=$(docker run -d $IMAGE /bin/bash)
docker export $CONTAINER | sudo tar xp -C $MOUNTPOINT

# Create /etc/hostname and /etc/resolv.conf in rootfs.
# Overwritten by firestarter so included here
# for illustration purposes only
#echo $HOSTNAME | sudo tee $MOUNTPOINT/etc/hostname >/dev/null
#sudo cp /run/systemd/resolve/resolv.conf $MOUNTPOINT/etc/resolv.conf
#sudo chmod 644 $MOUNTPOINT/etc/hostname $MOUNTPOINT/etc/resolv.conf

# Unmount, then tidy up the container and mountpoint.
sudo umount $MOUNTPOINT
rm -rf $MOUNTPOINT
docker rm $CONTAINER
```
Note that rather than:
```
docker export $CONTAINER | sudo tar xp -C $MOUNTPOINT
```
one could use:
```
sudo docker cp $CONTAINER:/ $MOUNTPOINT/
```
noting that in both cases using sudo is required because the earlier `sudo mount` changes the permissions of the rootfs directory.

### Create a filesystem with mount and docker create
There are a number of issues with the previous approach of using `mount` and `docker run` to extract the root filesystem.

Using mount is *far from ideal* as it requires elevated privileges and mount is particularly problematic if one wishes to use it from a container, because in addition to requiring additional capabilities mount is gated by the default Docker seccomp and AppArmor profiles. Moreover, the approach of extracting the root filesystem from a running container is hard to make generic. With this example we simply run /bin/bash, but clearly containers could be developed to do fairly arbitrary processing, so we cannot rely on /bin/bash to even exist in the images that we wish to convert.

More significantly though, given that a key use case for adopting Firecracker in preference to containers is to mitigate the risk of potentially malicious content, **one really should not start a container simply to extract the image contents**.

Rather than using `docker run` the following [mount-and-docker-create](mount-and-docker-create) script uses `docker create`, which will *create* the container **but not start it**.
```
SIZE=250
IMAGE=focal-demo
# To create a Docker style 12 hex character hostname use
# the following:
# HOSTNAME=$(tr -dc 'a-f0-9' < /dev/urandom | head -c12)
HOSTNAME="firecracker"
MOUNTPOINT=rootfs
FILESYSTEM=rootfs.ext4

# Prepare empty file of $SIZE MiB to accomodate filesystem
dd if=/dev/zero of=$FILESYSTEM bs=1M count=$SIZE

# Create an empty ext4 filesystem in the file
mkfs.ext4 $FILESYSTEM

# Create a mountpoint and mount our filesystem
mkdir -p $MOUNTPOINT
sudo mount $FILESYSTEM $MOUNTPOINT

# Create a container for our Docker image, then export the
# container's filesystem to our mountpoint
CONTAINER=$(docker create $IMAGE)
docker export $CONTAINER | sudo tar xp -C $MOUNTPOINT

# Create /etc/hostname and /etc/resolv.conf in rootfs.
# Overwritten by firestarter so included here
# for illustration purposes only
#echo $HOSTNAME | sudo tee $MOUNTPOINT/etc/hostname >/dev/null
#sudo cp /run/systemd/resolve/resolv.conf $MOUNTPOINT/etc/resolv.conf
#sudo chmod 644 $MOUNTPOINT/etc/hostname $MOUNTPOINT/etc/resolv.conf

# Unmount, then tidy up the container and mountpoint.
sudo umount $MOUNTPOINT
rm -rf $MOUNTPOINT
docker rm $CONTAINER
```
Our approach using `docker create` is clearly very similar to that using `docker run`, the only difference is that in this case we are using:
```
CONTAINER=$(docker create $IMAGE)
```
instead of:
```
CONTAINER=$(docker run -d $IMAGE /bin/bash)
```
which is a small, but nevertheless significant, difference.

### Create a filesystem using docker create without mounting
Although using `docker create` removes the need to actually *start* the container, the previous approach still requires mount. Using mount is problematic if we wish to run our filesystem creation script in a container, because whilst becoming root in a container is easy (if undesireable) [mount](https://man7.org/linux/man-pages/man2/mount.2.html) requires CAP_SYS_ADMIN privileges and is also gated by the default Docker seccomp and AppArmor profiles.

The following [docker-create-no-mount](docker-create-no-mount) script avoids using mount, and indeed **requires no elevated privileges at all**.

The "[trick](https://unix.stackexchange.com/questions/423965/how-to-create-a-file-system-as-a-non-root-user)" is to use the `-d` option of [mkfs.ext4](https://man7.org/linux/man-pages/man8/mke2fs.8.html), which allows us to specify a root directory to use for our filesystem. If used on its own by an unprivileged user however, it will result in incorrect file ownership in the target filesystem. A second "trick" therefore is to call mkfs.ext4 from a [fakeroot](https://manpages.debian.org/buster/fakeroot/fakeroot.1.en.html) environment. fakeroot was specifically written to enable users to create Debian GNU/Linux packages (in the deb format) without giving them root privileges, but it is also useful for creating other archive formats and filesystems.
```
SIZE=250
IMAGE=focal-demo
# To create a Docker style 12 hex character hostname use
# the following:
# HOSTNAME=$(tr -dc 'a-f0-9' < /dev/urandom | head -c12)
HOSTNAME="firecracker"
MOUNTPOINT=rootfs
FILESYSTEM=rootfs.ext4

# Create a directory for our filesystem
mkdir -p $MOUNTPOINT

# Run a container for our Docker image, then export the
# container's filesystem to our directory
CONTAINER=$(docker create $IMAGE)
docker export $CONTAINER | tar xp -C $MOUNTPOINT

# Create /etc/hostname and /etc/resolv.conf in rootfs.
# Overwritten by firestarter so included here
# for illustration purposes only
#echo $HOSTNAME | sudo tee $MOUNTPOINT/etc/hostname >/dev/null
#sudo cp /run/systemd/resolve/resolv.conf $MOUNTPOINT/etc/resolv.conf
#sudo chmod 644 $MOUNTPOINT/etc/hostname $MOUNTPOINT/etc/resolv.conf

# Create filesystem from directory contents using mkfs.ext4
# Use fakeroot to ensure filesystem has root:root ownership
# https://manpages.debian.org/buster/fakeroot/fakeroot.1.en.html
# https://man7.org/linux/man-pages/man8/mke2fs.8.html
fakeroot sh -c "mkfs.ext4 -L '' -N 0 -d ${MOUNTPOINT} -m 5 -r 1 ${FILESYSTEM} ${SIZE}M"

rm -rf $MOUNTPOINT
docker rm $CONTAINER
```

### Create a filesystem from a Docker image
The previous approaches in this tutorial have all focused on creating a root filesystem from a Docker *container* and whilst using `docker create` allows us to do that without actually running the container, it is still not ideal for our purposes. Creating the filesystem directly from Docker *images* is arguably more secure and additionally allows image metadata like ENTRYPOINT, ENV and WORKDIR to be exposed.

Using a Docker image as the source of a filesystem is somewhat more complex than using a container, because rather than being a simple single archive, an image comprises a set of filesystem *layers*, a configuration file, and a manifest describing the ordering of the layers as described in the [OCI Image Format Specification](https://github.com/opencontainers/image-spec/blob/main/spec.md).

The two main ways to obtain a Docker image are to either use `docker pull` to pull the image from a registry (e.g. Docker Hub or a private registry), or to use `docker save` to save a locally held image to a tar archive. Of those approaches pulling an image from a repository is the more complex and whilst we could do `docker pull` followed by `docker save` it is worth exploring how to pull an image directly from a registry *without* using Docker at all.

In order to pull an image the first thing to do is to parse the supplied image name. An [image name](https://docs.docker.com/engine/reference/commandline/tag/#extended-description) comprises a number of slash-separated name components, optionally prefixed by a registry hostname. The hostname must comply with standard DNS rules, but may not contain underscores. If a hostname is present, it may optionally be followed by a port number in the format `:8080`. If a registry hostname is not present, the command uses Docker’s public registry located at `registry-1.docker.io` by default. 
```
parse_image_name() {
  local image=$1
  local prefix=$([ -z "${image##*/*}" ] && echo $image | cut -d"/" -f1)
  if [[ $prefix =~ [\.:]|localhost ]]; then
    # Remove registry prefix to normalise image name
    image=$(echo $image | cut -d"/" -f2)
    local registry_URL="https://$prefix"
  else
    # Add "library" prefix if image isn't namespaced
    [ -z $prefix ] && image="library/$image"
    registry_URL="https://registry-1.docker.io"
  fi

  # Parse tag and image from normalised image name
  if [ -z "${image##*:*}" ]; then
    local tag=$(echo $image | cut -d":" -f2)
    image=$(echo $image | cut -d":" -f1)
  else
    local tag="latest"
  fi

  echo "$registry_URL $image $tag"
}
```
After parsing the image name we may use the registry URL, normalised image name and image tag to retrieve the [image manifest](https://docs.docker.com/registry/spec/manifest-v2-2/), which first requires that we retrieve the [bearer authentication token](https://docs.docker.com/registry/spec/auth/jwt/).

For Docker Hub the token may be obtained as follows:
```
get_dockerhub_auth_token() {
  local auth_URL="https://auth.docker.io"
  local service="registry.docker.io"
  local token=$(curl -fsSL "${auth_URL}/token?service=${service}&scope=repository:${image}:pull" | jq --raw-output .token)
  echo "$token"
}
```
Note that this method of obtaining a bearer authentication token will only work for Docker Hub and for images hosted in private registries it is likely that an alternative method of obtaining tokens will be required. AWS ECR for example has its own [GetAuthorizationToken](https://docs.aws.amazon.com/AmazonECR/latest/APIReference/API_GetAuthorizationToken.html) API that may also be accessed via the AWS CLI [get-authorization-token](https://awscli.amazonaws.com/v2/documentation/api/latest/reference/ecr/get-authorization-token.html) command, e.g. `aws ecr get-authorization-token`.

Given the bearer authentication token and the registry URL, normalised image name and image tag information previously parsed from the image name, we may now retrieve the image manifest as follows:
```
curl_ca_verification() {
  [[ $1 != "https://registry-1.docker.io" ]] && echo "--insecure"
}

get_manifest() {
  local URL=$1
  local token=$2
  local image=$3
  local digest=$4

  local query=$(curl -fsSL $(curl_ca_verification $URL) \
    -H "Authorization: Bearer $token" \
    -H 'Accept: application/vnd.docker.distribution.manifest.list.v2+json' \
    -H 'Accept: application/vnd.docker.distribution.manifest.v1+json' \
    -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
    "${URL}/v2/${image}/manifests/${digest}")

  if [[ $(echo $query | jq --raw-output 'has("manifests")') == true ]]; then
    digest=$(echo $query | jq --raw-output '.manifests[] | select(.platform.architecture=="amd64") | select(.platform.os=="linux") | .digest')
    get_manifest $URL $token $image $digest
  else
    echo $query
  fi
}
```
This function simply uses `curl` to retrieve the manifest, however using the default query to the URL will return the old v1 manifest so we add Accept headers to request the v2 manifest if available. Another point of note is that some images may have variants for different platforms (amd64, arm64 etc.). To account for this we first query the [manifest list](https://docs.docker.com/registry/spec/manifest-v2-2/#manifest-list) and if that is returned we select the digest from the linux+amd64 version and use that to do a second `get_manifest` call.

Another point of note here is the `curl_ca_verification` call. This tells curl to *not* verify the peer. It is included to account for the case that simple private registries using the `registry:2` image are often set up to use self-signed certificates. Rather that setting curl's `--insecure` flag as we do here, a more secure option is to use `--cacert [file]`to point to the required certificate location, or add the CA cert for your registry to the existing default CA certificate store e.g. /etc/ssl/certs.

Given the [manifest](https://docs.docker.com/registry/spec/manifest-v2-2/), it is now possible to query the image configuration object and filesystem layers, which are content-addressable via the digest field from the manifest's "config" and "layers" sections.

We first create a function to help us retrieve the image's config or layer blobs as indexed by the digest.
```
get_blob() {
  local URL=$1
  local token=$2
  local image=$3
  local digest=$4

  curl -fsSL $(curl_ca_verification $URL) \
    -H "Authorization: Bearer $token" \
    "${URL}/v2/${image}/blobs/${digest}"
}
```
To retrieve the [image configuration](https://github.com/opencontainers/image-spec/blob/main/config.md), which contains metadata like ENTRYPOINT, ENV and WORKDIR information, we may do:
```
local config=$(echo "$manifest" | jq --raw-output .config.digest)
config=$(get_blob $registry_URL $token $image $config)
[[ $config == "" ]] && exit 1

#echo $config > config.json # Raw JSON
echo $config | jq --raw-output . > config.json # Pretty print 
```
The process to get the layers is similar, but requires us to iterate through each layer in turn, building each filesystem layer on top of the last:
```
# Get the layers from the manifest
local layers=$(echo "$manifest" | jq --raw-output .layers[])
local layer_digests=$(echo "$layers" | jq --raw-output .digest)
local sizes=$(echo "$layers" | jq --raw-output .size)
sizes=(${sizes})  # Get layer sizes as an array

local unwritable_dirs=() # Used to restore original permissions
for digest in $layer_digests; do
  # Convert layer size to MB
  local layer_size=$(echo "scale=2; (${sizes[0]}+5000)/1000000" | bc)
  sizes=(${sizes[@]:1}) # Remove first element using sub-arrays
  echo -en "${digest:7:12}: Downloading ${layer_size}MB"
  get_blob $registry_URL $token $image $digest | tar -xz -C ${rootfs}
  echo -en "\r${digest:7:12}: Pull complete        \n"
  
  unwritable_dirs+=($(chmod_unwritable_dirs "$rootfs"))
  delete_marked_items ${rootfs}
done
```
The `delete_marked_items` call is interesting. The reason for it is because the [OCI layer specification](https://github.com/opencontainers/image-spec/blob/main/layer.md#whiteouts) represents deleted files or directories with a marker file prefixed with `.wh.`, so we must first delete any item marked in this way then remove the marker file.
```
delete_marked_items() {
  local rootfs=$1

  for item in $(find $rootfs -type f -name ".wh.*"); do
    rm -rf ${item/.wh./} # Remove marked path
    rm -rf $item # Remove marker file
  done
}
```
The `unwritable_dirs` array and `chmod_unwritable_dirs` call are interesting too. The issue is that some images might have made some directories unwritable, which wouldn't be an issue if we were untarring image layers as root, but because we want to run as an unprivileged user we must find any unwritable directories and make them writable otherwise untarring subsequent layers will fail with a permission error:
```
chmod_unwritable_dirs() {
  local rootfs=$1
  local unwritable_dirs=$(find $rootfs -type d ! -writable)

  for item in $unwritable_dirs; do chmod u+w $item; done
  echo "$unwritable_dirs"
}
```
However, we also want to make those directories unwritable again in our rootfs after we have finished unpacking all of the layers, so we record the paths of the directories that we have made writable into the `unwritable_dirs` array and iterate that after unpacking the layers:
```
for item in "${unwritable_dirs[@]}"; do chmod u-w $item; done
```
The procedure for extracting the manifest, configuration and layers from a tarred image file obtained from `docker save` is very similar to that followed when pulling an image from a registry, though obviously we are extracting with tar rather than using curl to pull from a URI.
```
docker_load() {
  local image=$1
  local rootfs=$2
  local decompress=""
  [ -z "${image##*.tar.gz*}" ] && decompress="z"
  echo "Loading image from from $image"

  # Get the image manifest from the registry. This is
  # similar, but not identical to the manifest described in: 
  # https://docs.docker.com/registry/spec/manifest-v2-2/
  local manifest=$(tar -${decompress}xOf $image manifest.json)
  #echo $manifest

  # Get the image config from the manifest.
  local config=$(echo "$manifest" | jq --raw-output .[0].Config)
  config=$(tar -${decompress}xOf $image $config)
  [[ $config == "" ]] && exit 1

  #echo $config > config.json # Raw JSON
  echo $config | jq --raw-output . > config.json # Pretty print with jq

  # Get the layers from the manifest
  local layers=$(echo "$manifest" | jq --raw-output .[0].Layers[])
  #echo $layers

  local unwritable_dirs=() # To restore original permissions
  for layer in $layers; do
    echo -en "${layer:0:12}: Untarring"
    tar -${decompress}xOf $image $layer | tar -x -C ${rootfs}
    echo -en "\r${layer:0:12}: Untar complete        \n"
    
    unwritable_dirs+=($(chmod_unwritable_dirs "$rootfs"))
    delete_marked_items "$rootfs"
  done
  echo
  
  # If there were any unwritable dirs in the layers that we
  # needed to make writable in order to unpack the filesysem,
  # we now restore them back to their original permissions.
  for item in "${unwritable_dirs[@]}"; do chmod u-w $item; done

  chmod_unreadable_dirs "$rootfs"
}
```
The complete script is in [image2fs](image2fs) and this again makes use of the [mkfs.ext4](https://man7.org/linux/man-pages/man8/mke2fs.8.html) `-d` option and  [fakeroot](https://manpages.debian.org/buster/fakeroot/fakeroot.1.en.html) to avoid the need for mount or any elevated privileges when creating the filesystem.

The [image2fs](image2fs) script developed in this tutorial serves as the basis of the approach used in [image-builder](../image-builder).
