//
// Copyright 2023 The GUAC Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	csubclient "github.com/guacsec/guac/pkg/collectsub/client"
	"github.com/guacsec/guac/pkg/collectsub/datasource"
	"github.com/guacsec/guac/pkg/collectsub/datasource/csubsource"
	"github.com/guacsec/guac/pkg/collectsub/datasource/inmemsource"
	"github.com/guacsec/guac/pkg/handler/collector"
	"github.com/guacsec/guac/pkg/handler/collector/deps_dev"
	"github.com/guacsec/guac/pkg/logging"
	"github.com/regclient/regclient/types/ref"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type depsDevOptions struct {
	// datasource for the collector
	dataSource datasource.CollectSource
	// address for NATS connection
	natsAddr string
	// run as poll collector
	poll bool
}

var depsDevCmd = &cobra.Command{
	Use:   "deps_dev [flags] purl1 purl2...",
	Short: "takes purls and queries them against deps.dev to find additional metadata to add to GUAC graph",
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := logging.WithLogger(context.Background())
		logger := logging.FromContext(ctx)

		opts, err := validateDepsDevFlags(
			viper.GetString("natsaddr"),
			viper.GetString("csub-addr"),
			viper.GetBool("use-csub"),
			args)
		if err != nil {
			fmt.Printf("unable to validate flags: %v\n", err)
			_ = cmd.Help()
			os.Exit(1)
		}

		if os.Getenv("DEPS_DEV_APIKEY") == "" {
			logger.Errorf("DEPS_DEV_APIKEY is not set")
			os.Exit(1)
		}
		depsToken := os.Getenv("DEPS_DEV_APIKEY")

		// Register collector
		depsDevCollector, err := deps_dev.NewDepsCollector(ctx, depsToken, opts.dataSource, opts.poll, 30*time.Second)
		if err != nil {
			logger.Errorf("unable to register oci collector: %v", err)
		}
		err = collector.RegisterDocumentCollector(depsDevCollector, deps_dev.DepsCollector)
		if err != nil {
			logger.Errorf("unable to register oci collector: %v", err)
		}

		initializeNATsandCollector(ctx, opts.natsAddr)
	},
}

func validateDepsDevFlags(natsAddr string, csubAddr string, useCsub bool, args []string) (depsDevOptions, error) {
	var opts depsDevOptions
	opts.natsAddr = natsAddr

	if useCsub {
		opts.poll = true
		c, err := csubclient.NewClient(csubAddr)
		if err != nil {
			return opts, err
		}
		opts.dataSource, err = csubsource.NewCsubDatasource(c, 10*time.Second)
		return opts, err
	}

	// else direct CLI call, no polling
	opts.poll = false
	if len(args) < 1 {
		return opts, fmt.Errorf("expected positional argument for image_path")
	}

	sources := []datasource.Source{}
	for _, arg := range args {
		if _, err := ref.New(arg); err != nil {
			return opts, fmt.Errorf("image_path parsing error. require format repo:tag")
		}
		sources = append(sources, datasource.Source{
			Value: arg,
		})
	}

	var err error
	opts.dataSource, err = inmemsource.NewInmemDataSources(&datasource.DataSources{
		OciDataSources: sources,
	})
	if err != nil {
		return opts, err
	}

	return opts, nil
}

func init() {
	rootCmd.AddCommand(depsDevCmd)
}
