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
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
)

// The global config information, shareable by all context objects.
// NOTE: The data in this struct should be considered read-only!
type GlobalConfig struct {
	LoopCount   int32
	TestCases   []*WeightRange
	Sequential  bool // Specify whether tests should be run sequentially (else it'd be randomly picked)
	DryRun      bool // If true, providers should print, but not run commands.
	MaxClusters int32
	MaxApps     int32
}

// The per go routine context for running tests. Provides a number of
// objects that are safe to be used within the go routine, along with some
// pre-configured loggers, etc.
type Context struct {
	Index    int32
	Config   *GlobalConfig
	Log      *log.Logger
	Rand     *rand.Rand
	Provider CloudProvider
	Clusters *ClusterManager
}

type ClusterHost struct {
	Master  string
	Minions []string
}

func NewContext(idx int32, clusters *ClusterManager, provider CloudProvider, config *GlobalConfig, rand *rand.Rand) *Context {
	return &Context{
		Index:    idx,
		Config:   config,
		Log:      log.New(os.Stdout, fmt.Sprintf("[ctx-%v] ", idx), log.LstdFlags|log.Lshortfile),
		Rand:     rand,
		Provider: provider,
		Clusters: clusters,
	}
}

// Enum for cluster state
type ClusterState int32

const (
	// Clean cluster, recently created, no previous state.
	ClusterClean ClusterState = iota

	// Cluster is/has been used. Some stuff may be there, but can be shared/used.
	ClusterDirty

	// Cluster is in exclusive use. Not to be used by others.
	ClusterExclusive

	// A catch all "any cluster". Not a cluster state, but useful for generic query for any clusters.
	ClusterAny
)

type ClusterManager struct {
	clusters map[string]*ClusterInfo
	mutex    *sync.RWMutex

	// Set of ports currently in use. Currently global.
	ports map[uint16]uint16

	// Set of firewall rules created/in-use.
	firewallRules map[string]*FirewallRuleInfo

	// Variables to use to account for pending creates.
	pendingClusterCreate int32
	pendingAppCreate     int32
}

type FirewallRuleInfo struct {
	ClusterName string
	Port        uint16
}

type ClusterInfo struct {
	State    ClusterState
	Services []string
	TestUrls []string
}

func NewClusterManager() *ClusterManager {
	return &ClusterManager{
		clusters:      map[string]*ClusterInfo{},
		mutex:         new(sync.RWMutex),
		ports:         map[uint16]uint16{},
		firewallRules: map[string]*FirewallRuleInfo{},
	}
}

// The WeightRange struct is used to provide a weighted range for a given value. Individually,
// they each represent the end value of its range and the data it represents. The beginning
// of the range is determined from the previous WeightRange's "End" value + 1. All ranges
// are inclusive. As such, this struct is only ever useful in an array format.
// For example, if the following values had the given weights:
//
//   a : 1
//   b : 4
//   c : 5
//
// The resulting []WeightRange representing their ranges would be:
//   [{End: 0, Value: a}, {End: 4, Value: b}, {End: 9, Value: c}]
//
// A random number generator can be used to then select a value by modding its value with the end
// range + 1:
//
//   r := rand.Int31() % (wr[len(wr) - 1].End + 1)
//
// A quick walk up the array to find the entry corresponding to the array will net the selected data.
//
type WeightRange struct {
	// The end value of this range. The beginning of the range is the previous range's "End + 1".
	// (or zero, if the first).
	End int32

	// The optional value it holds.
	Value string
}

type ByWeight []*WeightRange

func (wn ByWeight) Len() int {
	return len(wn)
}

func (wn ByWeight) Swap(i, j int) {
	wn[i], wn[j] = wn[j], wn[i]
}

func (wn ByWeight) Less(i, j int) bool {
	return wn[i].End < wn[j].End
}

func SelectRandomEntry(weights []*WeightRange, rand *rand.Rand) (string, error) {
	max := weights[len(weights)-1].End + 1
	hit := rand.Int31() % max

	for _, r := range weights {
		if hit <= r.End {
			return r.Value, nil
		}
	}
	return "", fmt.Errorf("Unexpected error. Selected value: %v, Max value: %v", hit, max)
}

// Context functions

func (ctx *Context) SelectRandomCase() (string, error) {
	return SelectRandomEntry(ctx.Config.TestCases, ctx.Rand)
}

func (ctx *Context) TryIncrementPendingClusterCreate() bool {
	mgr := ctx.Clusters
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	clusterTotal := mgr.pendingClusterCreate + int32(len(mgr.clusters))
	if clusterTotal < ctx.Config.MaxClusters {
		mgr.pendingClusterCreate++
		return true
	}

	return false
}

func (mgr *ClusterManager) Put(name string, state ClusterState) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	if info, ok := mgr.clusters[name]; ok {
		info.State = state
	} else {
		mgr.clusters[name] = &ClusterInfo{
			State:    state,
			Services: make([]string, 0, 1),
			TestUrls: make([]string, 0, 1),
		}

		// For any newly created clusters, decrement the pending cluster count
		if mgr.pendingClusterCreate > 0 {
			mgr.pendingClusterCreate--
		} else {
			// This shouldn't happen, but it's typically not fatal.
			// If you see this error, try to find the offending code path and add a
			// "TryIncrementPendingCluster" call to it to keep this accounting correct.
			fmt.Printf("!!! New cluster created w/o first incrementing pending cluster count. !!!")
		}
	}
}

func (ctx *Context) Query(query ClusterState) (string, error) {
	mgr := ctx.Clusters
	mgr.mutex.RLock()
	defer mgr.mutex.RUnlock()

	matchingClusters := make([]string, 0, len(mgr.clusters))
	for name, info := range mgr.clusters {
		if (query == ClusterAny && info.State != ClusterExclusive) || query == info.State {
			matchingClusters = append(matchingClusters, name)
		}
	}

	if len(matchingClusters) > 0 {
		retName := matchingClusters[ctx.Rand.Int31()%int32(len(matchingClusters))]
		return retName, nil
	}

	return "", fmt.Errorf("No cluster maching queried state: %v", query)
}

func (ctx *Context) QueryAndUpdate(query, update ClusterState) (string, error) {
	mgr := ctx.Clusters
	mgr.mutex.RLock()
	defer mgr.mutex.RUnlock()

	matchingClusters := make([]string, 0, len(mgr.clusters))
	for name, info := range mgr.clusters {
		if (query == ClusterAny && info.State != ClusterExclusive) || query == info.State {
			matchingClusters = append(matchingClusters, name)
		}
	}

	if len(matchingClusters) > 0 {
		retName := matchingClusters[ctx.Rand.Int31()%int32(len(matchingClusters))]
		mgr.clusters[retName].State = update
		return retName, nil
	}

	return "", fmt.Errorf("No cluster maching queried state: %v", query)
}

func (mgr *ClusterManager) Delete(name string) error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	info, ok := mgr.clusters[name]
	if ok {
		if info.State != ClusterExclusive {
			fmt.Errorf("Warning: Deleting non-exclusive cluster: %v", name)
		}
		delete(mgr.clusters, name)
	}

	// Requesting a delete of a non-existing cluster is a no-op
	return nil
}

func (mgr *ClusterManager) PurgeAll() (allClusters map[string]*ClusterInfo) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	allClusters = map[string]*ClusterInfo{}
	for name, info := range mgr.clusters {
		allClusters[name] = info
	}
	mgr.clusters = map[string]*ClusterInfo{}

	return
}

func (mgr *ClusterManager) GetPort() (uint16, error) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	// TODO(fabioy): A really dumb algorithm to find an unused port. Replace with better one later.
	for i := uint16(10000); i < 20000; i++ {
		if _, ok := mgr.ports[i]; !ok {
			mgr.ports[i] = i
			return i, nil
		}
	}
	return 0, fmt.Errorf("No free ports.")
}

func (mgr *ClusterManager) FreePort(port uint16) {
	delete(mgr.ports, port)
}

func (mgr *ClusterManager) AddService(clusterName, serviceName string) error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	info, ok := mgr.clusters[clusterName]
	if !ok {
		return fmt.Errorf("Unknown cluster name: %v", clusterName)
	}
	for _, name := range info.Services {
		if name == serviceName {
			return fmt.Errorf("Duplicate service name: %v", serviceName)
		}
	}
	info.Services = append(info.Services, serviceName)
	return nil
}

func (mgr *ClusterManager) RemoveService(clusterName, serviceName string) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	info, ok := mgr.clusters[clusterName]
	if !ok {
		fmt.Errorf("Unknown cluster name: %v", clusterName)
	}
	for i, name := range info.Services {
		if name == serviceName {
			info.Services = append(info.Services[:i], info.Services[i+1:]...)
		}
	}
}

func (ctx *Context) TryIncrementPendingAppCreate() bool {
	mgr := ctx.Clusters
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	// It's not perfect, but the number of test URLs is taken as the number of apps
	// installed.
	appTotal := mgr.pendingAppCreate
	for _, info := range mgr.clusters {
		appTotal += int32(len(info.TestUrls))
	}

	if appTotal < ctx.Config.MaxApps {
		mgr.pendingAppCreate++
		return true
	}

	return false
}

func (mgr *ClusterManager) AddTestUrl(clusterName, urlStr string) error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	info, ok := mgr.clusters[clusterName]
	if !ok {
		return fmt.Errorf("Unknown cluster name: %v", clusterName)
	}
	info.TestUrls = append(info.TestUrls, urlStr)

	// For any newly created app, decrement the pending app count
	if mgr.pendingAppCreate > 0 {
		mgr.pendingAppCreate--
	} else {
		// This shouldn't happen, but it's typically not fatal.
		// If you see this error, try to find the offending code path and add a
		// "TryIncrementPendingAppCreate" call to it to keep this accounting correct.
		fmt.Printf("!!! New app created w/o first incrementing pending app count. !!!")
	}

	return nil
}

func (ctx *Context) GetRandomTestUrl() (testName string, err error) {
	mgr := ctx.Clusters
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	// Create a slice containing all the available test cases and pick one at random.
	allUrls := make([]string, 0, len(mgr.clusters))
	for _, info := range mgr.clusters {
		if len(info.TestUrls) > 0 {
			allUrls = append(allUrls, info.TestUrls...)
		}
	}
	if len(allUrls) == 0 {
		err = fmt.Errorf("No available test url.")
		return
	}

	idx := ctx.Rand.Int31() % int32(len(allUrls))
	testName = allUrls[idx]
	return
}

func (mgr *ClusterManager) AddFirewallRule(name string, info *FirewallRuleInfo) error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	if _, ok := mgr.firewallRules[name]; ok {
		return fmt.Errorf("Duplicate firewall rule name: %v", name)
	}
	mgr.firewallRules[name] = info

	return nil
}

func (mgr *ClusterManager) PurgeAllFirewallRules() (purgedRules map[string]*FirewallRuleInfo) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()

	purgedRules = map[string]*FirewallRuleInfo{}
	for k, v := range mgr.firewallRules {
		purgedRules[k] = v
	}

	mgr.firewallRules = map[string]*FirewallRuleInfo{}
	return
}
