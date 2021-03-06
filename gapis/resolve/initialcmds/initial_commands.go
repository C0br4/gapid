// Copyright (C) 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package initialcmds

import (
	"context"

	"github.com/google/gapid/core/app/benchmark"
	"github.com/google/gapid/core/math/interval"
	"github.com/google/gapid/gapis/api"
	"github.com/google/gapid/gapis/capture"
	"github.com/google/gapid/gapis/database"
	"github.com/google/gapid/gapis/service/path"
)

var initialCommandsBuildCounter = benchmark.Duration("initialcmds.build")

type initialCommandData struct {
	cmds   []api.Cmd
	ranges interval.U64RangeList
}

// InitialCommands resolves and returns the Intial Commands for the capture C
func InitialCommands(ctx context.Context, c *path.Capture) ([]api.Cmd, interval.U64RangeList, error) {
	obj, err := database.Build(ctx, &InitialCmdsResolvable{c})
	if err != nil {
		return nil, nil, err
	}
	x := obj.(*initialCommandData)
	return x.cmds, x.ranges, nil
}

func (r *InitialCmdsResolvable) Resolve(ctx context.Context) (interface{}, error) {
	c, err := capture.ResolveFromPath(ctx, r.Capture)

	if err != nil {
		return nil, err
	}
	cmds, ranges := c.GetInitialCommands(ctx)
	return &initialCommandData{
		cmds, ranges}, nil
}
