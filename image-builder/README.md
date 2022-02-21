# image-builder
This directory provides [image-builder](image-builder), a tool to transform regular Docker images into a root filesystem for Firecracker and a directory containing the kernel, root filesystem, and Dockerfile needed to build the firecracker-in-docker image created from the source image.

## Usage
N.B. a working Firecracker kernel, e.g. as built by [kernel-builder](../kernel-builder), is required before using image-builder.

To use image-builder properly, either update the PATH environment variable to include the [image-builder](../image-builder) directory, or create a symlink to the image-builder executable from one of the directories already on PATH.

The basic usage is:
```
image-builder <name>
```
That will build a root filesystem from the specified source image `<name>` and create a `firecracker-<name>` directory containing the Dockerfile, kernel, root filesystem etc. The generated Dockerfile may be used to create a `firecracker-<name>` image in the usual way.

As a simple concrete example, use the `hello-world` image mentioned in the [Docker installation documentation](https://docs.docker.com/engine/install/ubuntu/) that is used to check if the Docker installation was successful:
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

## Implementation Details
The following steps are performed by the image-builder.

### Fetch and unpack image to create root filesystem
The image-builder tool is derived from the [image2fs](../firecracker-rootfs-tutorial/image2fs) example from the [firecracker-rootfs-tutorial](../firecracker-rootfs-tutorial). The majority of the code used to actually generate the root filesystem is identical to that example and described in detail in the firecracker-rootfs-tutorial, so won't be repeated here.

### Generate init
Once the basic root filesystem has been created, in most cases we next need to generate a simple init system for the Firecracker MicroVM guest to use.

On Linux and other Unix based operating systems init is the first user process started by the kernel during the boot process. Init is a daemon process that continues running until the system is shut down and is typically assigned process identifier 1 (PID 1).

With Docker, containers use [Linux namespaces](https://en.wikipedia.org/wiki/Linux_namespaces) as one of the isolation mechanisms. [PID namespaces](https://man7.org/linux/man-pages/man7/pid_namespaces.7.html) isolate the process ID number space, meaning that processes in different PID namespaces can have the same PID. Moreover, PIDs in a new PID namespace start at 1 meaning that running Docker containers spawns processes beginning with PID 1, so the ENTRYPOINT of a container will be its PID 1 process.

We would ideally like applications running in our firecracker-in-docker guest MicroVMs to behave in a similar way to how they do when run as regular containers, that is to say running as the PID 1 process. In theory we could simply configure the guest kernel to use the containerised application as init directly, however in most cases we also have to consider additional information like environment variables, WORKDIR, hostname, CMD and command line arguments, etc.

To cater for these additional requirements we generate a simple init script for the Firecracker guest, so that it inits in a similar way to a Docker container.

If the Docker image specifies an ENTRYPOINT of /sbin/init we use that directly. If, however, the image specifies a regular application as the ENTRYPOINT or CMD, as is more typical with Docker images, we generate a simple init-entrypoint script that will set the ENV vars specified in the image or passed at run time and also set WORKDIR, hostname, mount /proc, then finally exec the specified ENTRYPOINT/CMD application so that it replaces the init-entrypoint script as PID 1.

The `generate_init` function is fairly long, so we shall explain it in stages below.

We first extract the [config](https://github.com/opencontainers/image-spec/blob/main/config.md#properties) JSON document, which was itself extracted from the [manifest](https://docs.docker.com/registry/spec/manifest-v2-2/) document that was retrieved earlier as we were unpacking the image to create the root filesystem.

If the WorkingDir field (that represents WORKDIR) is empty or unset in the image config, we default workdir to /
```
local rootfs=$1
local config=$2

local config=$(echo "$config" | jq --raw-output .config)
local workdir=$(echo "$config" | jq --raw-output .WorkingDir)

[[ $workdir == "" || $workdir == "null" ]] && workdir="/"
```
We next extract any ENV, CMD and ENTRYPOINT values that might have been set in the image into bash arrays.
```
local SAVEIFS=$IFS # Save current IFS
IFS=$'\n'    # Change IFS to new line
local ENV=($(echo "$config" | jq --raw-output 'try .Env[]'))
local CMD=($(echo "$config" | jq --raw-output 'try .Cmd[]'))
local ENTRYPOINT=($(echo "$config" | jq --raw-output 'try .Entrypoint[]'))
IFS=$SAVEIFS # Restore IFS
```

When extracting these fields from the config document we convert JSON arrays to bash arrays [using jq try to treat null as an empty array](https://stackoverflow.com/a/54974102).

We also temporarily change the [Internal Field Separator](https://en.wikipedia.org/wiki/Input_Field_Separators) (IFS) to newline so that array conversion doesn't split on spaces, because we want e.g.
```
"Cmd":["/bin/sh","-c","echo \"Hello World\""]
```
to be parsed to
```
[/bin/sh, -c, echo "Hello World"]
```
and **not**
```
[/bin/sh, -c, echo, "Hello, World"]
```
After extracting the ENV, CMD and ENTRYPOINT values into arrays we test if ENTRYPOINT_OVERRIDE is set. This variable is set by the `--entrypoint` command line option and allows us to override the ENTRYPOINT specified in the image with another value. This option is mainly useful when trying to get images to work in Firecracker where we may wish to us a shell like `/bin/sh` as a temporary ENTRYPOINT.
```
if [ ! -z ${ENTRYPOINT_OVERRIDE+x} ]; then
  echo "Warning: --entrypoint=$ENTRYPOINT_OVERRIDE option overrides image ENTRYPOINT"
  ENTRYPOINT=("$ENTRYPOINT_OVERRIDE")
  CMD=()
fi
```

We next check whether ENTRYPOINT or CMD is /sbin/init and if so we will just use that, because init systems like systemd will set the hostname and mount /proc themselves.
```
if [[ "${ENTRYPOINT[@]}" == "/sbin/init" ]]; then
  echo "ENTRYPOINT is /sbin/init, using that"
elif [[ "${#ENTRYPOINT[@]}" == 0 && "${CMD[@]}" == "/sbin/init" ]]; then
  echo "CMD is /sbin/init, using that"
```
If /sbin/init is not specified, we create a simple init script to set the env, hostname, workir, mount /proc and exec the actual ENTRYPOINT, noting the use of exec as we want that command and not our init-entrypoint script as PID 1 in the Firecracker MicroVM guest.

The init-entrypoint that we will generate here, to init the guest to the ENTRYPOINT specified by the container image, is currently a simple shell script, so requires /bin/sh to run and /bin/hostname to initialise the guest's hostname. If those are not present in the root filesystem (e.g. in the case of scratch and other minimal images), we download and install the executables from busybox using the `check_and_install` function. This will also cache the busybox executables locally, because although they are small for some reason the downloads are quite slow:
```
check_and_install() {
  local rootfs=$1
  local executable=$2

  if [ ! -f "${rootfs}${executable}" ]; then
    local name=$(basename "$executable")
    echo "Warning: rootfs has no ${executable}, installing ${name} from busybox"
    mkdir -p $rootfs/bin

    if [ ! -d "busybox_cache" ]; then
      local busybox_path="https://www.busybox.net/downloads/binaries/1.30.0-i686"
      mkdir -p busybox_cache
      echo "Downloading busybox_ASH to local cache"
      curl -fsSL "$busybox_path/busybox_ASH" -o busybox_cache/sh
      echo "Downloading busybox_HOSTNAME to local cache"
      curl -fsSL "$busybox_path/busybox_HOSTNAME" -o busybox_cache/hostname
      echo "Downloading busybox_CAT to local cache"
      curl -fsSL "$busybox_path/busybox_CAT" -o busybox_cache/cat
      echo "Downloading busybox_MOUNT to local cache"
      curl -fsSL "$busybox_path/busybox_MOUNT" -o busybox_cache/mount
      echo "Downloading busybox_CHPST to local cache"
      curl -fsSL "$busybox_path/busybox_CHPST" -o busybox_cache/chpst
      chmod +x busybox_cache/*
    fi
    cp busybox_cache/${name} ${rootfs}${executable}
  fi
}
```
In due course it might be worth creating an init-entrypoint that is a standalone static executable binary to better support scratch images.

We use `check_and_install` as follows, which will install the required executables into the guest's root filesystem if they don't already exist:
```
check_and_install $rootfs /bin/sh
check_and_install $rootfs /bin/hostname
check_and_install $rootfs /bin/cat
check_and_install $rootfs /bin/mount
# setpriv or unshare would probably be more obvious than chpst
# https://man.archlinux.org/man/busybox.1.en#chpst
# but busybox versions of those don't support setting uid/euid
check_and_install $rootfs /bin/chpst
```
To generate the init-entrypoint script, we start by setting the shebang to `#!/bin/sh`then generate the environment variables specified in the image by iterating our ENV array.
```
local shell="#!/bin/sh\n"
# Set ENV vars for init script
local env=""
for entry in "${ENV[@]}"; do env="${env}export \"${entry}\"\n"; done
```
We then generate code to [source](https://en.wikipedia.org/wiki/Dot_(command)) the file that we will use for injecting ENV vars at run-time via Docker's -e/--env flags, testing if it exists first:
```
env="${env}[ -f /etc/profile.d/01-container-env-vars.sh ] && . /etc/profile.d/01-container-env-vars.sh\n"
```
We next generate code to set hostname, WORKDIR, and mount /proc for the init script, and make sure /tmp has the correct 1777 permissions:
```
local misc="[ -d /proc ] && mount -t proc proc /proc\n[ -d /tmp ] && chmod 1777 /tmp \nhostname "'$(cat /etc/hostname)'"\ncd $workdir\n"
```
We then generate code to set the ENTRYPOINT for the init script. This also extracts any command line args for init that may have been packed into the INIT_ARGS enviroment variable by [firestarter](launcher/resources/firestarter) in lieu of being properly set in the boot parameters.
```
local entrypoint=""
for entry in "${ENTRYPOINT[@]}"; do
  entrypoint="${entrypoint} "'\"'"${entry}"'\"'""
done

local cmd=""
for entry in "${CMD[@]}"; do
  cmd="${cmd} "'\"'"${entry}"'\"'""
done

local execute="# Transform the INIT_ARGS env var into "real" args\n"
if [[ "${#ENTRYPOINT[@]}" == 0 ]]; then
  execute="${execute}"'eval "set -- $INIT_ARGS"\n# After converting into "real" args remove INIT_ARGS from the environment\nunset INIT_ARGS\n# If any args have been passed use those, otherwise use CMD\n[ -z "$1" ] && eval "set -- '"${cmd}"'"\n'
else
  execute="${execute}"'eval "set -- '"${entrypoint} ${cmd}"' $INIT_ARGS"\n# After converting into "real" args remove INIT_ARGS from the environment\nunset INIT_ARGS\n'
fi
```
Finally, we actually generate the complete /sbin/init-entrypoint from the fragments that we created earlier and make it executable.
```
mkdir -p $rootfs/sbin # Make sure /sbin directory exists
# Need to use chpst to set uid as busybox setpriv and
# unshare don't support setting uid/euid.
echo -e "${shell}${env}${misc}${execute}"'if [ "$UID" = 0 ]; then\n  exec "$@"\nelse\n  exec chpst -u "$UID":0 "$@"\nfi' > $rootfs/sbin/init-entrypoint
chmod 755 $rootfs/sbin/init-entrypoint
```
An example generated /sbin/init-entrypoint script for a simple Ubuntu based image with no ENTRYPOINT or CMD explicitly set looks like:
```
#!/bin/sh
export "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
[ -f /etc/profile.d/01-container-env-vars.sh ] && . /etc/profile.d/01-container-env-vars.sh
[ -d /proc ] && mount -t proc proc /proc
[ -d /tmp ] && chmod 1777 /tmp
hostname $(cat /etc/hostname)
cd /
# Transform the INIT_ARGS env var into real args
eval "set -- $INIT_ARGS"
# After converting into "real" args remove INIT_ARGS from the environment
unset INIT_ARGS
# If any args have been passed use those, otherwise use CMD
[ -z "$1" ] && eval "set --  \"/bin/bash\""
if [ "$UID" = 0 ]; then
  exec "$@"
else
  exec chpst -u "$UID":0 "$@"
fi
```
### Generate and minimise the root filesystem
After unpacking the image to create the root filesystem contents then generating /sbin/init-entrypoint, we next need to create the actual ext4 root filesystem.

With regular Docker containers the container’s writable layer will simply
 grow until the underlying filesystem limits are reached, but a MicroVM
 requires its own filesystem which needs to be set to a specific size.

At build time the image-builder, by default, deliberately shrinks the MicroVM root filesystem to contain only those blocks that are actually  used in order to minimise image size. If the `-s` option is used, then the user may specify a particular filesystem size to use, in which case image-builder will use that size rather than shrinking, which is likely to result in a larger overall image size.
```
if [ $SIZE -eq 0 ]; then
  # Estimate required minimum rootfs size (disk usage + 20%)
  # The du -shm command returns summarised directory size in MB
  FILESYSTEM_SIZE=$(du -shm $MOUNTPOINT | grep -o '[0-9]*' | head -1)
  FILESYSTEM_SIZE="$((FILESYSTEM_SIZE + (FILESYSTEM_SIZE / 5)))M"
  echo "Estimated filesystem size: ${FILESYSTEM_SIZE}"
else
  FILESYSTEM_SIZE=$SIZE
  echo "Requested filesystem size: ${FILESYSTEM_SIZE}"
fi
```
We next create a filesystem of the specified size from the directory contents, using [mkfs.ext4](https://man7.org/linux/man-pages/man8/mke2fs.8.html) to create the filesystem and [fakeroot](https://manpages.debian.org/buster/fakeroot/fakeroot.1.en.html) to ensure the filesystem has root:root ownership:
```
rm -f $FILESYSTEM
set +e # Temporarily disable exit on error to ensure mountpoint tidy up happens
fakeroot sh -c "mkfs.ext4 -L '' -N 0 -d ${MOUNTPOINT} -m 5 -r 1 ${FILESYSTEM} ${FILESYSTEM_SIZE}"
```
If the filesystem is successfully created then if the user hasn't specified a particular size we attempt to shrink the root filesystem to its minimum size. We do this by getting the *actual* number of blocks that were created (by parsing the output of `e2fsck -n`) and resizing to that using [resize2fs](https://man7.org/linux/man-pages/man8/resize2fs.8.html).
```
if [[ $SIZE == "0" ]]; then
  FSCK_RESULTS=($(e2fsck -n $FILESYSTEM))
  BLOCKS=$(echo ${FSCK_RESULTS[4]} | cut -d"/" -f1)

  # Shrink root filesystem to minimise the final image size.
  resize2fs -f $FILESYSTEM $BLOCKS
fi
```
### Generate the firecracker-in-docker image directory
After creating the ext4 root filesystem for the image that we are transforming, we next generate a directory containing the kernel, root filesystem, and Dockerfile needed to build the firecracker-in-docker image created from the source image.

The `generate_firecracker_in_docker` function starts off by working out the absolute path of the [launcher](../launcher) and setting the name of the target image:
```
local image=$1
local rootfs=$2
template_dir=$(echo "$(get_script_dir)/../launcher")
target="firecracker-$image"
```
To work out the absolute path of launcher of we use the `get_script_dir` function, based on a [stackoverflow example](https://stackoverflow.com/a/246128) to find the *actual* directory that this script is running from.

The motivation for this is because we want to be able to use the launcher directory as a "template" for the Dockerfiles that we will be generating for the images being transformed into firecracker-in-docker images, so we need to know where to find the launcher directory even if we're running image-builder from a symlink to it created on our PATH.
```
get_script_dir() {
  local source="${BASH_SOURCE[0]}"
  # While $source is a symlink, resolve it
  while [ -h "$source" ]; do
    local dir="$(cd -P "$(dirname "$source")" && pwd)"
    source="$(readlink "$source")"
    # If $source was a relative symlink (so no "/" as prefix,
    # we need to resolve it relative to the symlink base dir
    [[ $source != /* ]] && source="$dir/$source"
  done
  echo "$(cd -P "$(dirname "$source")" && pwd)"
}
```
Once we have got the `template_dir` and `target` name, we create the target directory and move the root filesystem and copy .dockerignore to it, then make the root filesystem writable.
```
mkdir -p $target/rootfs
cp -n $template_dir/.dockerignore $target/.dockerignore
mv $rootfs $target/rootfs/$rootfs
chmod 666 $target/rootfs/$rootfs
```
We next check whether a Dockerfile already exists in the target directory. If not we then check if the user has specified generating a "standalone" Dockerfile or one based on a firecracker-in-docker base image.

If we're creating a standalone Dockerfile we copy the kernel and firestarter ENTRYPOINT, then copy the template Dockerfile from the launcher Directory and modify it (using sed) to COPY rootfs.ext4 into the image.
```
mkdir -p $target/kernel $target/resources
if [ -f $template_dir/kernel/vmlinux ]; then
  cp $template_dir/kernel/vmlinux $target/kernel/vmlinux
else
  echo "Error: generating $target failed, could not find $template_dir/kernel/vmlinux"
  exit 1
fi
# -n "no clobber" copy (e.g. copy but don't overwrite)
cp -n $template_dir/resources/firestarter $target/resources/firestarter
cp -n $template_dir/Dockerfile $target/Dockerfile
sed -i "s/\/usr\/local\/bin\/vmlinux/\/usr\/local\/bin\/vmlinux\nCOPY rootfs\/rootfs.ext4 \/usr\/local\/bin\/rootfs.ext4/g" $target/Dockerfile
sed -i "s/firecracker-in-docker/$target/g" $target/Dockerfile
```
If we've specified a base image, we generate a new Dockerfile to specify the base image and COPY the rootfs.ext4 into the image, which will look something like:
```
FROM firecracker-in-docker

COPY rootfs/rootfs.ext4 /usr/local/bin/rootfs.ext4
```
Finally, we copy and rename the template `docker run` launch script:
```
if [ ! -f $target/$target ]; then
  cp $template_dir/firecracker $target/$target
  sed -i "s/firecracker-in-docker/$target/g" $target/$target
fi
```
which will look something like:
```
docker run --rm -it \
  --cap-drop all \
  --cap-add NET_RAW \
  --cap-add NET_ADMIN \
  --group-add $(cut -d: -f3 < <(getent group kvm)) \
  --device=/dev/kvm \
  --device /dev/net/tun \
  -u $(id -u):$(id -g) \
  firecracker-hello-world
```
and generate a template README.md for the directory
