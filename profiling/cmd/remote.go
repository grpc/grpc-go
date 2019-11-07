/*
 *
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/profiling"
	"google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
	"io"
	"os"
	"time"
)

func setEnabled(ctx context.Context, c pspb.ProfilingClient, enabled bool) error {
	_, err := c.SetEnabled(ctx, &pspb.SetEnabledRequest{Enabled: enabled})
	if err != nil {
		grpclog.Printf("error calling SetEnabled: %v\n", err)
		return err
	}

	grpclog.Printf("successfully set enabled = %v", enabled)
	return nil
}

func retrieveSnapshot(ctx context.Context, c pspb.ProfilingClient, f string) error {
	grpclog.Infof("establishing stream stats stream")
	stream, err := c.GetStreamStats(ctx, &pspb.GetStreamStatsRequest{})
	if err != nil {
		grpclog.Errorf("error calling GetStreamStats: %v\n", err)
		return err
	}

	s := &snapshot{StreamStats: make([]*profiling.Stat, 0)}

	grpclog.Infof("receiving and processing stream stats")
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				grpclog.Infof("received EOF, last message")
				break
			}
			grpclog.Errorf("error recv: %v", err)
			return err
		}

		stat := proto.StatProtoToStat(resp)
		s.StreamStats = append(s.StreamStats, stat)
	}

	grpclog.Infof("creating snapshot file %s", f)
	file, err := os.Create(f)
	defer file.Close()
	if err != nil {
		grpclog.Errorf("cannot create %s: %v", f, err)
		return err
	}

	grpclog.Infof("encoding data and writing to snapshot file %s", f)
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(s)
	if err != nil {
		grpclog.Infof("error encoding: %v", err)
		return err
	}

	grpclog.Infof("successfully wrote profiling snapshot to %s", f)
	return nil
}

func remoteCommand() error {
	ctx := context.Background()
	if *flagTimeout > 0 {
		ctx, _ = context.WithTimeout(context.Background(), time.Duration(*flagTimeout)*time.Second)
	}

	grpclog.Infof("dialing %s", *flagAddress)
	cc, err := grpc.Dial(*flagAddress, grpc.WithInsecure())
	if err != nil {
		grpclog.Errorf("cannot dial %s: %v", *flagAddress, err)
		return err
	}
	defer cc.Close()

	c := pspb.NewProfilingClient(cc)

	if *flagEnableProfiling || *flagDisableProfiling {
		return setEnabled(ctx, c, *flagEnableProfiling)
	} else if *flagRetrieveSnapshot {
		return retrieveSnapshot(ctx, c, *flagSnapshot)
	} else {
		return fmt.Errorf("what should I do with the remote target?")
	}
}
