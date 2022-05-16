# kvm-device-plugin
It is trivial to expose `/dev/kvm` to regular Docker containers simply by specifying `--device = /dev/kvm` in the `docker run` command. Unfortunately, when using Kubernetes to orchestrate containers there is no simple equivalent of Docker's `--device` due to the distributed, heterogenous nature of Kubernetes nodes.

Fortunately, Kubernetes provides a [DevicePlugin framework](https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/device-plugin.md) that may be used to advertise system hardware resources to the Kubelet.

This directory provides a simple [Kubernetes DevicePlugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/) to expose `/dev/kvm` to containers running in Kubernetes.

The code is largely based on the [examples](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#examples) section of the Kubernetes DevicePlugin documentation and the Go [DevicePlugin API](https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1) documentation.

## Prerequisites
When using older versions of Kubernetes it may be necessary to open the required feature gate using Kubelet's `--feature-gates=DevicePlugins=true`

A working installation of `make` is required to use the bundled Makefile.

To build locally an up to date version of Go (preferably 1.18 or higher) is required, though the Dockerised build does not require this.

## Building kvm-device-plugin
To build locally (requires a working Go installation):
```
make
```
To build with Docker:
```
make docker-build
```
or to avoid using make simply run:
```
docker build -t kvm-device-plugin .
```
## Usage