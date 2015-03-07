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

package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fabioy/kubernetes/test/stress/cases"
	"github.com/fabioy/kubernetes/test/stress/common"
	"github.com/fabioy/kubernetes/test/stress/provider"
)

var (
	loop_count         = flag.Int("loop-count", 1, "Number of times to run a particular test per test instance.")
	rand_seed          = flag.Int64("rand-seed", int64(0), "Random number seed (0 to use current system time).")
	sequential_tests   = flag.Bool("seq-test", false, "Run tests in sequential order. Useful for verification testing.")
	dry_run            = flag.Bool("dry-run", false, "Issue the commands but do not execute them.")
	max_test_instances = flag.Int("max-test-instances", 1, "Sets the limit of concurrent tests running")
	go_max_procs       = flag.Int("go-max-procs", 0, "Sets the runtime.GOMAXPROCS for the process, 0 for default (number of CPUs).")
	skip_cleanup       = flag.Bool("skip-cleanup", false, "Skip the cleanup phase. Leaves all test resources created as is.")
	setup_cases        = flag.String("setup", "", "List of test cases to use for setup. Specify each case separated by a comma (i.e. \"createCluster,installTestApp)\"")
	max_clusters       = flag.Int("max-clusters", 10, "Maximum number of clusters to create. Default 10.")
	max_apps           = flag.Int("max-apps", 10, "Maximum number of tests to create. Default 10.")
)

func createRootRand() *rand.Rand {
	var randSeed int64
	if *rand_seed == int64(0) {
		randSeed = time.Now().Unix()
	} else {
		randSeed = *rand_seed
	}

	log.Printf("Random seed: %v", randSeed)

	return rand.New(rand.NewSource(randSeed))
}

func printUsage() {
	allTestCases := cases.GetAllTestCases()
	tests := make([]string, 0, len(allTestCases))
	for k := range allTestCases {
		tests = append(tests, k)
	}
	sort.Strings(tests)

	fmt.Println("\n\nUsage: stress-driver [options] tests")
	fmt.Println("Multiple <tests> can be specified, i.e. createCluster listCluster.")
	fmt.Println("Tests may also have a weight, which is used if randomization is selected, i.e. pingTestApp#2 sleep#10")
	fmt.Println("Available test cases: ", strings.Join(tests, ", "))
	fmt.Println("Options:")
	flag.PrintDefaults()

	os.Exit(1)
}

func resolveGlobalConfig(allTestCases cases.AllTestCases) *common.GlobalConfig {
	test_cases := flag.Args()
	if len(test_cases) <= 0 {
		printUsage()
	}
	testRanges, err := ParseTestCases(test_cases, *sequential_tests, allTestCases)
	if err != nil {
		fmt.Println(err)
		printUsage()
	}

	loopCount := int32(*loop_count)
	if loopCount <= 0 {
		fmt.Println("Invalid loop-count value: %v", loopCount)
		printUsage()
	}

	maxClusters := int32(*max_clusters)
	if maxClusters <= 0 {
		fmt.Printf("Invalid max-clusters value: %v", maxClusters)
		printUsage()
	}

	maxApps := int32(*max_apps)
	if maxApps <= 0 {
		fmt.Printf("Invalid max-apps value: %v", maxApps)
		printUsage()
	}

	return &common.GlobalConfig{
		TestCases:   testRanges,
		LoopCount:   loopCount,
		Sequential:  *sequential_tests,
		DryRun:      *dry_run,
		MaxClusters: maxClusters,
		MaxApps:     maxApps,
	}
}

func main() {
	flag.Parse()

	allTestCases := cases.GetAllTestCases()

	globalConfig := resolveGlobalConfig(allTestCases)

	// Set the default logger prefix so that it can be identified (in case someone uses it).
	// If you need to log it from a go routine, use Context.Logger.
	log.SetPrefix("[main] ")

	// Parameter validations and config
	if *max_test_instances <= 0 {
		fmt.Printf("Invalid number of max-test-instances: %v", *max_test_instances)
		printUsage()
	}

	maxProcs := *go_max_procs
	if maxProcs < 0 {
		fmt.Println("Invalid go-max-procs value: %v", maxProcs)
		printUsage()
	} else if maxProcs == 0 {
		maxProcs = runtime.NumCPU()
	}
	runtime.GOMAXPROCS(maxProcs)

	// Create the root random number generator. Be careful when using this, Rand objects
	// are NOT thread-safe. Use Context.Rand object if used from a go routine.
	rootRand := createRootRand()

	clusterManager := common.NewClusterManager()

	// CONSIDER: This may be a good place to divvy up the test cases among the test routines.
	// For now, every Context object has the full list of test cases, from which it can randomly
	// pick one and run.
	// Context 0 is kept around for setup and cleanup work, before any of the test cases are run.
	ctxs := make([]*common.Context, *max_test_instances+1)
	for i := range ctxs {
		// TODO(fabioy): Currently hard coded to GKE. Need to make this configurable.
		provider := new(provider.GKECloudProvider)
		ctxs[i] = common.NewContext(int32(i), clusterManager, provider, globalConfig, rand.New(rand.NewSource(rootRand.Int63())))
	}

	runTests(ctxs)

	if *skip_cleanup {
		ctxs[0].Log.Printf("[Cleanup] Skipping cleanup.")
	} else {
		cleanUpAll(ctxs[0])
	}
}

func runTests(ctxs []*common.Context) {
	var wg sync.WaitGroup

	// Shared variable to indicate whether the test loop should exit now
	sharedExitNow := int32(0)

	for i, ctx := range ctxs {
		if i == 0 {
			setupAll(ctx)
		} else {
			wg.Add(1)
			go func(context *common.Context) {
				defer func() {
					// In case of panic, stop all other tests
					if err := recover(); err != nil {
						if atomic.LoadInt32(&sharedExitNow) == 0 {
							// There was a panic. Stop everyone else and wait for orderly shutdown.
							atomic.StoreInt32(&sharedExitNow, 1)

							fmt.Println("!!!! An error has occurred. Press Enter to continue with cleanup and exit. ")
							var b []byte = make([]byte, 1)
							os.Stdin.Read(b)
						}
					}
					wg.Done()
				}()

				for i := int32(0); i < context.Config.LoopCount && atomic.LoadInt32(&sharedExitNow) == 0; i++ {
					if context.Config.Sequential {
						for _, test := range context.Config.TestCases {
							testFunction, ok := cases.GetTestCase(test.Value)
							if !ok {
								context.Log.Fatalf("Unknown test case: %v", test.Value)
							}

							testFunction(context)
						}
					} else {
						testCase, err := context.SelectRandomCase()
						if err != nil {
							context.Log.Fatalf("Error fetching test case: %v", err)
						}

						// This code assumes that concurrent read-only access to the map is thread-safe.
						// The access around it may have to be protected if these assumptions change.
						testFunction, ok := cases.GetTestCase(testCase)
						if !ok {
							context.Log.Fatalf("Unknown test case: %v", testCase)
						}

						testFunction(context)
					}
				}

			}(ctx)
		}
	}

	wg.Wait()
}

// Placeholder for any setup needed before running test cases.
func setupAll(ctx *common.Context) {
	ctx.Log.Printf("[Setup] Starting setup.")
	// Parse and process the setup test cases
	if len(*setup_cases) > 0 {
		setupTests := strings.Split(*setup_cases, ",")

		allTestCases := cases.GetAllTestCases()
		for _, test := range setupTests {
			if _, ok := allTestCases[test]; !ok {
				fmt.Printf("Invalid test case: %v", test)
				printUsage()
			}
		}

		for _, test := range setupTests {
			testFunction, ok := cases.GetTestCase(test)
			if !ok {
				ctx.Log.Fatalf("Unknown test case: %v", test)
			}

			testFunction(ctx)
		}
	}

	ctx.Log.Printf("[Setup] Finish setup.")
}

// Cleans up any state from test cases. For now they include:
//   - Deleting all clusters that the tests may have created.
func cleanUpAll(ctx *common.Context) {
	ctx.Log.Printf("[Cleanup] Starting cleanup.")

	// Delete all firewalls
	firewallRules := ctx.Clusters.PurgeAllFirewallRules()
	firewallNames := make([]string, 0, len(firewallRules))
	for name := range firewallRules {
		firewallNames = append(firewallNames, name)
	}
	ctx.Log.Printf("[Cleanup] Firewall rules to delete[%v]: %v", len(firewallRules), strings.Join(firewallNames, ", "))
	for name, info := range firewallRules {
		ctx.Log.Printf("[Cleanup] Deleting firewall rule: %v", name)
		if err := ctx.Provider.DeleteFirewallRule(ctx, name, info); err != nil {
			ctx.Log.Printf("[Cleanup] Error deleting cluster: %v", name)
		}
	}

	// Delete all clusters and services
	clusters := ctx.Clusters.PurgeAll()
	clusterNames := make([]string, 0, len(clusters))
	for name := range clusters {
		clusterNames = append(clusterNames, name)
	}
	ctx.Log.Printf("[Cleanup] Clusters to delete[%v]: %v", len(clusterNames), strings.Join(clusterNames, ", "))
	for name, info := range clusters {
		for _, svc := range info.Services {
			ctx.Log.Printf("[Cleanup] Deleting service: %v", svc)
			if err := common.DeleteService(ctx, name, svc); err != nil {
				ctx.Log.Printf("[Cleanup] Error deleting service: %v", svc)
			}
		}
		ctx.Log.Printf("[Cleanup] Deleting cluster: %v", name)
		if err := ctx.Provider.DeleteCluster(ctx, name); err != nil {
			ctx.Log.Printf("[Cleanup] Error deleting cluster: %v", name)
		} else {
			ctx.Log.Printf("Successfully deleted service %v", name)
		}
	}
}

func ParseTestCases(args []string, isRunSequential bool, allTestCases cases.AllTestCases) (ranges []*common.WeightRange, err error) {
	weights := make([]*common.WeightRange, 0, len(args))

	// Start by parsing the test cases and weights and putting them into the array.
	seqWarned := false // Remember whether user has been warned of sequential run.
	for _, arg := range args {
		// Tests with no weight values get a default value of 1.
		// Also, if tests are to be run sequentially, all tests weights are ignored.
		weight := int32(1)
		var test string
		if strings.Contains(arg, "#") {
			vals := strings.Split(arg, "#")
			if len(vals) > 2 {
				err = fmt.Errorf("Invalid test case format: %v", arg)
				return
			}
			test = vals[0]

			if isRunSequential {
				if !seqWarned {
					fmt.Printf("WARNING: Test weights are ignored when tests are run sequentially.")
					seqWarned = true
				}
			} else {
				num, err := strconv.ParseInt(vals[1], 10, 32)
				if err != nil || num <= 0 {
					err = fmt.Errorf("Error parsing weight: %v, Error: %v", arg, err)
				}
				weight = int32(num)
			}
		}
		if _, ok := allTestCases[test]; !ok {
			err = fmt.Errorf("Invalid test case: %v", test)
		}
		weights = append(weights, &common.WeightRange{End: int32(weight), Value: test})
	}

	// Sort weights from least to most.
	if !isRunSequential {
		sort.Sort(common.ByWeight(weights))
	}

	// Fix up the "End" value wrt the previous entry. The result will be an ordered array
	// where each entry's range is the previous entry's "End+1" to that entry's "End" value.
	nextVal := int32(0)
	for i := range weights {
		r := weights[i].End
		weights[i].End = nextVal + r - 1
		nextVal += r
	}
	ranges = weights
	return
}
