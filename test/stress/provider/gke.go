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

package provider

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fabioy/kubernetes/test/stress/common"

	compute "code.google.com/p/google-api-go-client/compute/v1"
	container "code.google.com/p/google-api-go-client/container/v1beta1"
)

type GKECloudProvider struct{}

func (provider *GKECloudProvider) Init() error {
	return nil
}

func (provider *GKECloudProvider) CreateCluster(ctx *common.Context, clusterName string) error {
	ctx.Log.Printf("Creating cluster: %v", clusterName)
	params := fmt.Sprintf("-q --format json preview container clusters create %v --num-nodes=1", clusterName)

	output, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields(params)...))
	if err != nil {
		return err
	}

	var cluster container.Cluster
	if err = json.Unmarshal(output, &cluster); err != nil {
		ctx.Log.Printf("Error attempting to read server response:\n%v\nerror: %v", string(output), err)
	} else {
		ctx.Log.Printf("Cluster created: %v", cluster)
	}

	return nil
}

func (provider *GKECloudProvider) DeleteCluster(ctx *common.Context, clusterName string) error {
	ctx.Log.Printf("Deleting cluster: %v", clusterName)
	params := fmt.Sprintf("-q --format json preview container clusters delete %v", clusterName)

	output, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields(params)...))
	if err != nil {
		return err
	}

	var operation container.Operation
	if err = json.Unmarshal(output, &operation); err != nil {
		ctx.Log.Printf("Error attempting to read server response:\n%v\nerror: %v", string(output), err)
	} else {
		ctx.Log.Printf("Cluster delete: %v", operation)
	}

	return nil
}

// Simple wrapper to help with JSON deserialization.
type ClustersData struct {
	Clusters []container.Cluster
}

func (provider *GKECloudProvider) ListClusters(ctx *common.Context) (clusterNames []string, err error) {
	output, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields("-q --format json preview container clusters list")...))
	if err != nil {
		return
	}

	var clustersData ClustersData
	if err = json.Unmarshal(output, &clustersData); err != nil {
		ctx.Log.Printf("Error attempting to read server response:\n%v\nerror: %v", string(output), err)
		return
	}

	clusters := clustersData.Clusters
	clusterNames = make([]string, 0, len(clusters))
	for i, cluster := range clusters {
		clusterNames[i] = cluster.Name
		ctx.Log.Printf("Cluster name: %v, Status: %v", cluster.Name, cluster.Status)
	}

	return
}

func (provider *GKECloudProvider) CreateFirewallRule(ctx *common.Context, ruleName string, info *common.FirewallRuleInfo) error {
	ctx.Log.Printf("Creating firewall rule. Name: %v, cluster: %v, port: %v", ruleName, info.ClusterName, info.Port)
	params := fmt.Sprintf("-q --format json compute firewall-rules "+
		"create %v --allow tcp:%d --target-tags k8s-%v-node",
		ruleName, info.Port, info.ClusterName)
	_, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields(params)...))
	return err
}

func (provider *GKECloudProvider) DeleteFirewallRule(ctx *common.Context, ruleName string, info *common.FirewallRuleInfo) error {
	ctx.Log.Printf("Delete firewall rule. Name: %v, cluster: %v, port: %v", ruleName, info.ClusterName, info.Port)
	params := fmt.Sprintf("-q --format json compute firewall-rules delete %v", ruleName)
	_, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields(params)...))
	return err
}

func (provider *GKECloudProvider) RunKubectl(ctx *common.Context, cluster, params string) (string, error) {
	if len(cluster) != 0 {
		params = fmt.Sprintf("--cluster=%v %v", cluster, params)
	}
	params = fmt.Sprintf("preview container kubectl %v", params)
	output, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields(params)...))
	return string(output), err
}

func (provider *GKECloudProvider) ResolveClusterHosts(ctx *common.Context, clusterName string) (hosts *common.ClusterHost, err error) {
	ctx.Log.Printf("Resolve cluster hosts. Cluster: %v", clusterName)
	output, err := common.RunCmd(ctx, exec.Command("gcloud", strings.Fields("-q --format json compute instances list")...))
	if err != nil {
		return
	}

	var instances []compute.Instance
	if err = json.Unmarshal(output, &instances); err != nil {
		ctx.Log.Printf("Error attempting to read server response:\n%v\nerror: %v", string(output), err)
	}

	masterPrefix := fmt.Sprintf("k8s-%v-master", clusterName)
	minionPrefix := fmt.Sprintf("k8s-%v-node-", clusterName)
	hosts = &common.ClusterHost{
		Minions: make([]string, 0),
	}

	for _, instance := range instances {
		if strings.HasPrefix(instance.Name, masterPrefix) {
			hosts.Master = instance.Name
		} else if strings.HasPrefix(instance.Name, minionPrefix) {
			hosts.Minions = append(hosts.Minions, instance.Name)
		}
	}

	return
}
