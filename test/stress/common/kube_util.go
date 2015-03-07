/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	v1beta1 "github.com/GoogleCloudPlatform/kubernetes/pkg/api/v1beta1"
)

// Some methods to help standardize the names for various objects.
func CreateRandomClusterName(ctx *Context) string {
	return fmt.Sprintf("cl-%d-%d", ctx.Index, ctx.Rand.Int31())
}

func CreateRandomPodName(ctx *Context) string {
	return fmt.Sprintf("p-%d-%d", ctx.Index, ctx.Rand.Int31())
}

func CreateFirewallRuleName(clusterName string, port uint16) string {
	return fmt.Sprintf("%v-node-%d", clusterName, port)
}

// Some nice kubectl wrappers for various operations.

func RunContainer(ctx *Context, clusterName, podName, image string) (output string, err error) {
	podParams := fmt.Sprintf("run-container %v "+
		"--image=%v --port=80 --replicas=1 --labels=tid=%v -o json",
		podName, image, podName)
	output, err = ctx.Provider.RunKubectl(ctx, clusterName, podParams)
	return
}

func CreateService(ctx *Context, clusterName, serviceName, podName string, port uint16) (svcOutput string, err error) {
	// TODO(fabioy): Add label once this flag is available (PR #5522).
	// --label=tid=%v
	svcParams := fmt.Sprintf("expose %v --service-name=%v --port=%d --selector=tid=%v "+
		"--container-port=80 --protocol=TCP --create-external-load-balancer -o json",
		podName, serviceName, port, podName)

	// Service creation seems to timeout often. Do a retry loop where we attempt to create it
	// with verification checks on it.
	var output string
	for retry := 0; retry < 5; retry++ {
		output, err = ctx.Provider.RunKubectl(ctx, clusterName, svcParams)
		if err != nil {
			ctx.Log.Printf("Error creating service: %v, Error: %v", output, err)
		}
		if DoesServiceExist(ctx, clusterName, serviceName) {
			err = nil
			break
		}
	}
	if err != nil {
		return
	}
	svcOutput = output
	if err = ctx.Clusters.AddService(clusterName, serviceName); err != nil {
		ctx.Log.Printf("Error adding service: %v, Error: %v", serviceName, err)
	}

	// Add label to service. This may be removed once the PR #5522 is merged.
	// Retry this a few times, since service creation can take a while.
	labelParams := fmt.Sprintf("label services %v tid=%v", serviceName, podName)
	output, err = ctx.Provider.RunKubectl(ctx, clusterName, labelParams)
	if err != nil {
		err = fmt.Errorf("Error adding label to service: %v", err)
		svcOutput = output
	}
	return
}

func GetExternalIp(ctx *Context, clusterName, podName string) (ipAddr string, err error) {
	svcIpParams := fmt.Sprintf("get services --selector=tid=%v -o json", podName)
	output, err := ctx.Provider.RunKubectl(ctx, clusterName, svcIpParams)
	if err != nil {
		return
	}

	var svcList v1beta1.ServiceList
	if err = json.Unmarshal([]byte(output), &svcList); err != nil {
		err = fmt.Errorf("Error attempting to read service external ip. Error: %v, Output: %v", err, string(output))
	}

	// TODO(fabioy): This needs some validation on the resulting object
	if len(svcList.Items) == 0 || len(svcList.Items[0].PublicIPs) == 0 {
		err = fmt.Errorf("Error attenting to retrieve service external ip: %v", string(output))
	} else {
		ipAddr = svcList.Items[0].PublicIPs[0]
	}
	return
}

func DoesServiceExist(ctx *Context, clusterName, serviceName string) bool {
	params := fmt.Sprintf("get services %v", serviceName)
	for retry := 0; retry < 10; retry++ {
		_, err := ctx.Provider.RunKubectl(ctx, clusterName, params)
		if err == nil {
			return true
		} else {
			ctx.Log.Printf("Waiting for service creation. Service: %v", serviceName)
			time.Sleep(2 * time.Second)
		}
	}
	return false
}

func DeleteService(ctx *Context, clusterName, serviceName string) error {
	params := fmt.Sprintf("delete services %v", serviceName)
	output, err := ctx.Provider.RunKubectl(ctx, clusterName, params)
	if err != nil {
		return fmt.Errorf("Error deleting service: %v, Error: %v", output, err)
	}
	return nil
}

func TestAppUrl(ctx *Context, appUrl string) (output string, err error) {
	resp, err := http.Get(appUrl)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	output = string(body)
	return
}
