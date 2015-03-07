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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func RunCmd(ctx *Context, cmd *exec.Cmd) (stdout []byte, err error) {
	ctx.Log.Printf("Running: %v %v", cmd.Path, strings.Join(cmd.Args[1:], " "))
	if ctx.Config.DryRun {
		return
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	stdout, err = cmd.Output()
	stderr := errBuf.Bytes()
	// TODO (Issue #5513): Until kubectl returns an non-zero error code for validation errors,
	// this extra check ensures the code prints the error.
	if err != nil || strings.HasPrefix(string(stderr), "ERROR:") {
		ctx.Log.Printf("Error running %v: %v", cmd.Path, err)
		ctx.Log.Printf("Output:\n%v", string(stdout))
		ctx.Log.Printf("Error:\n%v", string(stderr))

		// If err is nil, set an error object so code upstream can handle it properly.
		if err == nil {
			err = fmt.Errorf("Error running %v: %v", cmd.Path, string(stderr))
		}
	}
	return
}
