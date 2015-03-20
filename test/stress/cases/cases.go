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

package cases

import (
	"fmt"
	"time"

	"github.com/fabioy/kubernetes/test/stress/common"
)

var (
	// The global map of test case names to the test case functions.
	all_test_cases = map[string]TestCaseFunction{
		//	"createDeleteCluster": caseCreateDeleteCluster,
		"createCluster":  caseCreateCluster,
		"deleteCluster":  caseDeleteCluster,
		"installTestApp": caseInstallTestApp,
		"listCluster":    caseListCluster,
		"sleep":          caseSleep,
		"pingTestApp":    casePingTestApp,
		"listPods":       caseListPods,
	}
)

// All test cases should use this as their method signature.
// To be available to run, an entry to the "all_test_cases" map
// should be added.
type TestCaseFunction func(*common.Context)

// A small abstraction around the map. This is present in case in the
// future other data/configuration or objects are needed to handle test cases.
type AllTestCases map[string]TestCaseFunction

func GetAllTestCases() AllTestCases {
	return all_test_cases
}

func GetTestCase(testCase string) (TestCaseFunction, bool) {
	// This code assumes that concurrent read-only access to the map is thread-safe.
	// The access around it may have to be protected if these assumptions change.
	tf, ok := all_test_cases[testCase]
	return tf, ok
}

func caseListCluster(ctx *common.Context) {
	ctx.Log.Println("[Test case: listCluster]")
	clusters, err := ctx.Provider.ListClusters(ctx)
	if err != nil {
		ctx.Log.Printf("Error listing clusters: %v", err)
		return
	}
	ctx.Log.Printf("Number of clusters: %v", len(clusters))
	for _, cluster := range clusters {
		ctx.Log.Printf("Cluster name: %v", cluster)
	}
}

func caseCreateCluster(ctx *common.Context) {
	ctx.Log.Println("[Test case: createCluster]")

	if !ctx.TryIncrementPendingClusterCreate() {
		ctx.Log.Printf("Attempted to create cluster, but hit max limit. Returning.")
		return
	}

	name := CreateRandomClusterName(ctx)

	err := ctx.Provider.CreateCluster(ctx, name)
	if err != nil {
		ctx.Log.Printf("Error creating cluster: %v, Error: %v", name, err)
	} else {
		ctx.Clusters.Put(name, common.ClusterClean)
		ctx.Log.Printf("Successfully created cluster: %v", name)
	}
}

func caseDeleteCluster(ctx *common.Context) {
	ctx.Log.Println("[Test case: deleteCluster]")

	name, err := ctx.QueryAndUpdate(common.ClusterAny, common.ClusterExclusive)
	if err != nil {
		// No clusters available. Report and go on.
		ctx.Log.Println("Attempted to delete cluster, but none available.")
		return
	}

	if err := ctx.Provider.DeleteCluster(ctx, name); err != nil {
		ctx.Log.Panicf("Error deleting cluster: %v, Error: %v", name, err)
	} else {
		ctx.Clusters.Delete(name)
		ctx.Log.Printf("Successfully deleted cluster: %v", name)
	}
}

func caseSleep(ctx *common.Context) {
	ctx.Log.Println("[Test case: Sleep 1s]")
	time.Sleep(1 * time.Second)
}

func caseInstallTestApp(ctx *common.Context) {
	ctx.Log.Println("[Test case: installTestApp]")

	if !ctx.TryIncrementPendingAppCreate() {
		ctx.Log.Printf("Attempted to create app, but hit max limit. Returning.")
		return
	}

	clusterName, err := ctx.QueryAndUpdate(common.ClusterClean, common.ClusterDirty)
	if err != nil {
		clusterName, err = ctx.QueryAndUpdate(common.ClusterDirty, common.ClusterDirty)
	}
	if err != nil {
		ctx.Log.Printf("No available cluster: %v. Aborting test install.", err)
		return
	} else {
		ctx.Log.Printf("Using cluster: %v", clusterName)
	}

	// Create pod
	podName := CreateRandomPodName(ctx)
	output, err := common.RunContainer(ctx, clusterName, podName, "fabioy/helloworld")
	if err != nil {
		ctx.Log.Panicf("Error creating app pod: %v, Error: %v, Output: %v", podName, err, output)
		return
	}
	ctx.Log.Printf("Successfully created pod: %v, Output:%v", podName, output)

	// Create service (with external load balancer option)
	port, err := ctx.Clusters.GetPort()
	if err != nil {
		ctx.Log.Panicf("Error getting port. Error: %v", err)
		return
	}

	serviceName := fmt.Sprintf("svc-%v", podName)
	output, err = common.CreateService(ctx, clusterName, serviceName, podName, port)
	if err != nil {
		ctx.Log.Panicf("Error creating service: %v, Error: %v, Output: %v", serviceName, err, output)
	}
	ctx.Log.Printf("Successfully created service %v, output:\n%v", serviceName, output)

	// Retrieve the external IP of the service.
	externalIp, err := common.GetExternalIp(ctx, clusterName, podName)
	testUrl := fmt.Sprintf("http://%v:%v", externalIp, port)
	if err != nil {
		ctx.Log.Panic(err)
	} else {
		ctx.Log.Printf("Successfully got external IP, app test URL: %v", testUrl)
		ctx.Clusters.AddTestUrl(clusterName, testUrl)
	}

	// Create firewall rule
	ruleName := common.CreateFirewallRuleName(clusterName, port)
	clusterInfo := &common.FirewallRuleInfo{ClusterName: clusterName, Port: port}
	err = ctx.Provider.CreateFirewallRule(ctx, ruleName, clusterInfo)
	if err != nil {
		ctx.Log.Panicf("Error creating app firewall rule: %v", err)
	}
	if err = ctx.Clusters.AddFirewallRule(ruleName, clusterInfo); err != nil {
		ctx.Log.Panicf("Error adding firewall rule: %v", err)
	}
	ctx.Log.Printf("Successfully created firewall rule: %v", ruleName)

	// Issue request and print output
	output, err = common.TestAppUrl(ctx, testUrl)
	if err != nil {
		ctx.Log.Panicf("Error making request to app. URL: %v, Error: %v", testUrl, err)
	} else {
		ctx.Log.Printf("Successful request to app. URL: %v, Output: %v", testUrl, output)
	}

	ctx.Log.Println("Test setup complete!")
}

func casePingTestApp(ctx *common.Context) {
	ctx.Log.Println("[Test case: Ping Test App]")

	testUrl, err := ctx.GetRandomTestUrl()
	if err != nil {
		ctx.Log.Printf("No test url was available. Sleeping for a bit.")
		time.Sleep(1 * time.Second)
	}

	output, err := common.TestAppUrl(ctx, testUrl)
	if err != nil {
		ctx.Log.Panicf("Error making request to app. URL: %v, Error: %v", testUrl, err)
	} else {
		ctx.Log.Printf("Successful request to app. URL: %v, Output: %v", testUrl, output)
	}
}

func caseListPods(ctx *common.Context) {
	ctx.Log.Println("[Test case: List pods]")
	clusterName, err := ctx.Query(common.ClusterAny)
	output, err := common.ListPods(ctx, clusterName)
	if err != nil {
		ctx.Log.Panicf("Error listing pods, err: %v, output: %v", err, output)
	} else {
		ctx.Log.Printf("List pods: %v", output)
	}
}
