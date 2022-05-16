/*
 *
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 *
 */

package main

import (
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// KVMDevicePlugin implements the DevicePluginServer interface and represents
// a gRPC client/server.
// https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-implementation
// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#DevicePluginServer
type KVMDevicePlugin struct {
	devs         []*pluginapi.Device
	resourceName string
	devicePath   string
	socket       string
	stop         chan interface{}
	server       *grpc.Server
}

// Factory to create KVMDevicePlugin instance with numDevices available devices.
func NewKVMDevicePlugin(resourceNamespace string, devicePath string, numDevices int) *KVMDevicePlugin {
	// Get the device name from path, e.g. /dev/kvm yields kvm
	name := path.Base(devicePath)

	glog.V(3).Infof("Creating %s DevicePlugin on %s/%s with %d available devices.",
		devicePath, resourceNamespace, name, numDevices)

	var devs []*pluginapi.Device
	for i := 0; i < numDevices; i++ {
		devs = append(devs, &pluginapi.Device{
			ID:     strconv.Itoa(i),
			Health: pluginapi.Healthy,
		})
	}

	return &KVMDevicePlugin{
		devs:         devs,
		resourceName: resourceNamespace + "/" + name,
		devicePath:   devicePath,
		socket:       pluginapi.DevicePluginPath + resourceNamespace + "_" + name,
		stop:         make(chan interface{}),
	}
}

// dial establishes the gRPC communication with the registered device plugin.
func dial(timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(pluginapi.KubeletSocket,
		grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, err
	}

	return c, nil
}

// Start the gRPC server of the DevicePlugin.
func (plugin *KVMDevicePlugin) Start() error {
	devicePath := plugin.devicePath
	glog.V(3).Infof("Starting %s DevicePlugin gRPC server.", devicePath)

	err := plugin.cleanup()
	if err != nil {
		glog.Errorf("Failed to setup %s DevicePlugin gRPC server: %s.", devicePath, err)
		return err
	}

	sock, err := net.Listen("unix", plugin.socket)
	if err != nil {
		glog.Errorf("Failed to setup %s DevicePlugin gRPC server: %s.", devicePath, err)
		return err
	}

	plugin.server = grpc.NewServer([]grpc.ServerOption{}...)
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#RegisterDevicePluginServer
	pluginapi.RegisterDevicePluginServer(plugin.server, plugin)

	go plugin.server.Serve(sock)
	glog.V(3).Infof("Serving gRPC requests for %s DevicePlugin.", devicePath)

	// Wait for the server to start by launching a blocking connection.
	conn, err := dial(60 * time.Second)
	if err != nil {
		glog.Errorf("Failed to setup %s DevicePlugin gRPC server: %s.", devicePath, err)
		return err
	}
	conn.Close()

	//go m.healthcheck()

	return nil
}

// Stop the gRPC server.
// Trying to stop already stopped DevicePlugin emits an info-level log message.
func (plugin *KVMDevicePlugin) Stop() error {
	devicePath := plugin.devicePath
	if plugin.server == nil {
		glog.V(3).Infof("Tried to stop stopped %s DevicePlugin.", devicePath)
		return nil
	}

	glog.V(3).Infof("Stopping %s DevicePlugin gRPC server.", devicePath)
	plugin.server.Stop()
	plugin.server = nil
	close(plugin.stop)
	glog.V(3).Infof("%s DevicePlugin gRPC server has stopped.", devicePath)

	return plugin.cleanup()
}

// Register registers the device plugin (as a gRPC client call) for the given
// resourceName with the Kubelet DevicePlugin Registration gRPC service.
// https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-registration
func (plugin *KVMDevicePlugin) Register() error {
	devicePath := plugin.devicePath
	glog.V(3).Infof("Registering %s DevicePlugin with Kubelet.", devicePath)
	conn, err := dial(5 * time.Second)
	if err != nil {
		glog.Errorf("%s DevicePlugin could not dial gRPC: %s.", devicePath, err)
		return err
	}
	defer conn.Close()

	// A device plugin can register itself with the Kubelet through this gRPC
	// service. During the registration, the DevicePlugin needs to send:
	// * The name of its Unix socket.
	// * The DevicePlugin API version against which it was built.
	// * The ResourceName it wants to advertise.
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#NewRegistrationClient
	client := pluginapi.NewRegistrationClient(conn)

	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#RegisterRequest
	glog.Infof("Creating %s DevicePlugin RegisterRequest for endpoint %s.",
		devicePath, path.Base(plugin.socket))
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(plugin.socket),
		ResourceName: plugin.resourceName,
	}

	// https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-registration
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#RegistrationClient
	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		glog.Errorf("Registration of %s DevicePlugin failed: %s.", devicePath, err)
		glog.Errorf("Make sure that the DevicePlugins feature gate is enabled and Kubelet is running.")
		return err
	}
	return nil
}

// ListAndWatch sends gRPC stream of Devices.
// Whenever a Device state changes or a Device disappears, ListAndWatch
// returns the new list.
// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#UnimplementedDevicePluginServer.ListAndWatch
func (plugin *KVMDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	glog.V(3).Infof("ListAndWatch exposing %d device resources.", len(plugin.devs))
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#DevicePlugin_ListAndWatchServer
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#ListAndWatchResponse
	s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.devs})

	for {
		select {
		case <-plugin.stop:
			return nil
		}
	}
}

// Allocate allocates a set of devices to be used by container runtime environment.
func (plugin *KVMDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	devicePath := plugin.devicePath
	glog.V(3).Infof("%s DevicePlugin Allocate.", devicePath)

	devs := plugin.devs

	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#AllocateResponse
	responses := pluginapi.AllocateResponse{}

	// Iterate through ContainerRequests, creating a ContainerAllocateResponse
	// if a device with the requested device ID exists.
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#AllocateRequest
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#ContainerAllocateRequest
	for _, req := range reqs.ContainerRequests {
		glog.V(3).Infof("%s", req)
		// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#ContainerAllocateResponse
		// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#DeviceSpec
		response := pluginapi.ContainerAllocateResponse{
			Devices: []*pluginapi.DeviceSpec{
				&pluginapi.DeviceSpec{
					ContainerPath: devicePath,
					HostPath:      devicePath,
					Permissions:   "rw",
				},
			},
		}

		for _, id := range req.DevicesIDs {
			deviceExists := false
			for _, d := range devs {
				if d.ID == id {
					deviceExists = true
					break
				}
			}
			if !deviceExists {
				return nil, fmt.Errorf("Invalid AllocateRequest, unknown device: %s.", id)
			}
		}

		glog.V(3).Infof("%s", &response)
		responses.ContainerResponses = append(responses.ContainerResponses, &response)
	}

	return &responses, nil
}

// cleanup is a helper to remove the DevicePlugin's socket.
func (plugin *KVMDevicePlugin) cleanup() error {
	devicePath := plugin.devicePath
	glog.V(3).Infof("Removing socket %s for %s DevicePlugin.", plugin.socket, devicePath)
	if err := os.Remove(plugin.socket); err != nil && !os.IsNotExist(err) {
		glog.Errorf("Could not clean up socket %s for %s DevicePlugin: %s.",
			plugin.socket, devicePath, err)
		return err
	}

	return nil
}

// Serve starts the gRPC server and registers the DevicePlugin to Kubelet.
func (plugin *KVMDevicePlugin) Serve() error {
	devicePath := plugin.devicePath
	glog.V(3).Infof("Serving %s DevicePlugin.", devicePath)
	err := plugin.Start()
	if err != nil {
		glog.Errorf("Could not start %s DevicePlugin: %s.", devicePath, err)
		return err
	}
	glog.V(3).Infof("Starting to serve on %s.", plugin.socket)

	err = plugin.Register()
	if err != nil {
		glog.Errorf("Could not register %s DevicePlugin: %s.", devicePath, err)
		plugin.Stop()
		return err
	}
	glog.V(3).Infof("Registered %s DevicePlugin with Kubelet.", devicePath)

	return nil
}

// PreStartContainer is called, if indicated by DevicePlugin during registration
// phase, before each container start. The DevicePlugin can run device specific
// operations such as reseting the device before making devices available to the
// container.
func (plugin *KVMDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// GetPreferredAllocation returns a preferred set of devices to allocate from a
// list of available ones. The resulting preferred allocation is not guaranteed
// to be the allocation ultimately performed by the devicemanager. It is only
// designed to help the devicemanager make a more informed allocation decision
// when possible. GetPreferredAllocation was added in API v0.19.9.
func (plugin *KVMDevicePlugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

// GetDevicePluginOptions returns options to be communicated with Device Manager.
func (plugin *KVMDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}
