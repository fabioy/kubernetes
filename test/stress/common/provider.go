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

type CloudProvider interface {
	// Called before any other methods are invoked.
	// May be a good place to do any verifications for configuration or one-time
	// initializations for the provider.
	// Return an error object if initialization failed.
	Init() error

	// Methods to create, delete and list clusters.
	CreateCluster(ctx *Context, clusterName string) error
	DeleteCluster(ctx *Context, clusterName string) error
	ListClusters(ctx *Context) (clusterNames []string, err error)

	// Given a cluster name, return the hosts (VMs or machines) for that cluster.
	ResolveClusterHosts(ctx *Context, clusterName string) (hosts *ClusterHost, err error)

	// Methods to create or delete a firewall rule for a cluster
	CreateFirewallRule(ctx *Context, ruleName string, info *FirewallRuleInfo) error
	DeleteFirewallRule(ctx *Context, ruleName string, info *FirewallRuleInfo) error

	// Method to call kubectl. It's provided here in case the
	// individual provider needs to contain extra info or work to invoke it.
	RunKubectl(ctx *Context, cluster, params string) (stdout string, err error)
}
