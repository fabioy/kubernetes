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

	"github.com/fabioy/kubernetes/test/stress/common"
)

func CreateRandomClusterName(ctx *common.Context) string {
	return fmt.Sprintf("cl-%d-%d", ctx.Index, ctx.Rand.Int31())
}

func CreateRandomPodName(ctx *common.Context) string {
	return fmt.Sprintf("p-%d-%d", ctx.Index, ctx.Rand.Int31())
}

func deleteCluster(ctx *common.Context, name string) error {
	if err := ctx.Provider.DeleteCluster(ctx, name); err != nil {
		ctx.Log.Printf("Error deleting cluster: %v, Error: %v", name, err)
		return err
	}

	ctx.Clusters.Delete(name)
	ctx.Log.Printf("Successfully deleted cluster: %v", name)

	return nil
}

func createCluster(ctx *common.Context, name string) error {
	if err := ctx.Provider.CreateCluster(ctx, name); err != nil {
		ctx.Log.Printf("Error creating cluster: %v, Error: %v", name, err)
		return err
	}

	ctx.Clusters.Put(name, common.ClusterClean)
	ctx.Log.Printf("Successfully created cluster: %v", name)

	return nil
}
