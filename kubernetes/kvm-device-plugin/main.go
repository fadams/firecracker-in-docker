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

/*
This is a Kubernetes DevicePlugin to expose /dev/kvm to containers.
https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/
https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/device-plugin.md

The code is largely based on the examples section of the documentation above
https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#examples
and the Go DevicePlugin API documentation
https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1
*/

package main

import (
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/golang/glog"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	KVM_PATH                  = "/dev/kvm"
	RESOURCE_NAMESPACE        = "devices.kubevirt.io"
	AVAILABLE_DEVICES_DEFAULT = 1000
)

func main() {
	flag.Parse()

	glog.V(3).Infof("Starting %s DevicePlugin manager.", KVM_PATH)

	// Create OS signal notification channel to notify us of a subset of signals.
	glog.V(3).Info("Starting OS signal notification channel.")
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Create filesystem notification channel to notify us of changes to the
	// DevicePluginPath directory (/var/lib/kubelet/device-plugins/), which is
	// the directory the DevicePlugin is expecting sockets to be created on.
	// https://pkg.go.dev/k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1#pkg-constants
	glog.V(3).Info("Starting DevicePlugin socket directory notification channel.")
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Error("Failed to create DevicePlugin socket directory notification channel.")
		os.Exit(1)
	}
	defer fsWatcher.Close()
	fsWatcher.Add(pluginapi.DevicePluginPath)

	// The DevicePlugin API does not allow for infinitely available devices
	// like /dev/kvm, rather the DevicePlugin sends the Kubelet the list of
	// devices it manages, and the Kubelet is in charge of advertising those
	// resources to the API server as part of the Kubelet node status update.
	// https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-registration
	// The approach taken here is to register a configurable, or arbitrarily
	// large default, number of available devices.
	numDevices := AVAILABLE_DEVICES_DEFAULT
	if value, ok := os.LookupEnv("AVAILABLE_DEVICES"); ok {
		if intValue, err := strconv.Atoi(value); err == nil {
			numDevices = intValue
		}
	}

	restart := true
	var plugin *KVMDevicePlugin

	// Start a loop that will handle messages from opened channels.
	glog.V(3).Info("Handling incoming signals.")
HandleSignals:
	for {
		if restart {
			if plugin != nil {
				plugin.Stop()
			}

			if _, err := os.Stat(KVM_PATH); err == nil {
				glog.V(3).Infof("Device path %s is available on this node.", KVM_PATH)
				plugin = NewKVMDevicePlugin(RESOURCE_NAMESPACE, KVM_PATH, numDevices)
			} else {
				glog.Errorf("Device path %s not available on this node.", KVM_PATH)
				os.Exit(1)
			}

			if err := plugin.Serve(); err != nil {
				glog.V(3).Info("Could not contact Kubelet, retrying. Did you enable the DevicePlugin feature gate?")
			} else {
				restart = false
			}
		}

		select {
		case event := <-fsWatcher.Events:
			if event.Name == pluginapi.KubeletSocket &&
				event.Op&fsnotify.Create == fsnotify.Create {
				glog.V(3).Infof("Received inotify Create event: %s, restarting.",
					pluginapi.KubeletSocket)
				restart = true
			}

		case err := <-fsWatcher.Errors:
			glog.V(3).Infof("Received inotify error: %s.", err)

		case s := <-signalCh:
			switch s {
			case syscall.SIGHUP:
				glog.V(3).Info("Received SIGHUP, restarting.")
				restart = true
			default:
				glog.V(3).Infof("Received signal \"%v\", shutting down.", s)
				err := plugin.Stop()
				if err != nil {
					glog.Errorf("Failed to stop plugin's \"%s\" server: %s",
						plugin.devicePath, err)
				}

				break HandleSignals
			}
		}
	}
}
