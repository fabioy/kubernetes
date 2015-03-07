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
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/fabioy/kubernetes/test/stress/common"
	"github.com/fabioy/kubernetes/test/stress/provider"
)

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

func ParseTestCases(args []string) (ranges []*WeightRange, err error) {
	weights := make([]*WeightRange, 0, len(args))

	// Start by parsing the test cases and weights and putting them into the array.
	for _, arg := range args {
		if strings.Contains(arg, "#") {
			vals := strings.Split(arg, "#")
			if len(vals) > 2 {
				fmt.Printf("Invalid test case format: %v", arg)
				return
			}
			weight, err := strconv.ParseInt(vals[1], 10, 32)
			if err != nil || weight <= 0 {
				fmt.Printf("Error parsing weight: %v, Error: %v", arg, err)
			}
			weights = append(weights, &WeightRange{End: int32(weight), Value: vals[0]})
		} else {
			// Tests with no weight values get a default value of 1.
			weights = append(weights, &WeightRange{End: int32(1), Value: arg})
		}
	}

	// Sort weights from least to most.
	sort.Sort(ByWeight(weights))

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

func GetRandomCase(weights []*WeightRange, rand *rand.Rand) (string, error) {
	max := weights[len(weights)-1].End + 1
	hit := rand.Int31() % max

	for _, r := range weights {
		if hit <= r.End {
			return r.Value, nil
		}
	}
	return "", fmt.Errorf("Unexpected error. Selected value: %v, Max value: %v", hit, max)
}

func main() {
	flag.Parse()

	params := flag.Args()
	if len(params) == 0 {
		fmt.Println("Usage: scratch testName#weight...")
	}

	wr, err := ParseTestCases(params)
	fmt.Printf("Ranges: %v, Error: %v", wr, err)

	rand := rand.New(rand.NewSource(0))

	for i := 0; i < 10; i++ {
		fmt.Println(GetRandomCase(wr, rand))
	}
}

func DoResolveClusterHosts() {
	flag.Parse()

	params := flag.Args()
	provider := new(provider.GKECloudProvider)

	clusterManager := common.NewClusterManager()
	globalConfig := &common.GlobalConfig{
		TestCases:   []string{},
		LoopCount:   1,
		Sequential:  true,
		DryRun:      false,
		MaxClusters: 1,
		MaxApps:     1,
	}

	ctx := common.NewContext(0, clusterManager, provider, globalConfig, rand.New(rand.NewSource(0)))

	clusterHost, err := provider.ResolveClusterHosts(ctx, params[0])
	if err != nil {
		fmt.Printf("Error: %v", err)
	} else {
		fmt.Printf("Success!\n%v", clusterHost)
	}
}
