// Copyright 2017 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	. "github.com/pingcap/check"
	"github.com/pingcap/pd/server/core"
	"github.com/pingcap/pd/server/kv"

	// Register schedulers.
	_ "github.com/pingcap/pd/server/schedulers"
)

func Test(t *testing.T) {
	TestingT(t)
}

var _ = Suite(&testConfigSuite{})

type testConfigSuite struct{}

func (s *testConfigSuite) TestTLS(c *C) {
	cfg := NewConfig()
	tls, err := cfg.Security.ToTLSConfig()
	c.Assert(err, IsNil)
	c.Assert(tls, IsNil)
}

func (s *testConfigSuite) TestBadFormatJoinAddr(c *C) {
	cfg := NewConfig()
	cfg.Join = "127.0.0.1:2379" // Wrong join addr without scheme.
	c.Assert(cfg.Adjust(nil), NotNil)
}

func (s *testConfigSuite) TestReloadConfig(c *C) {
	opt, err := newTestScheduleOption()
	c.Assert(err, IsNil)
	storage := core.NewStorage(kv.NewMemoryKV())
	scheduleCfg := opt.Load()
	scheduleCfg.MaxSnapshotCount = 10
	opt.SetMaxReplicas(5)
	opt.LoadPDServerConfig().UseRegionStorage = true
	c.Assert(opt.Persist(storage), IsNil)

	// suppose we add a new default enable scheduler "adjacent-region"
	defaultSchedulers := []string{"balance-region", "balance-leader", "hot-region", "label", "adjacent-region"}
	newOpt, err := newTestScheduleOption()
	c.Assert(err, IsNil)
	newOpt.AddSchedulerCfg("adjacent-region", []string{})
	c.Assert(newOpt.Reload(storage), IsNil)
	schedulers := newOpt.GetSchedulers()
	c.Assert(schedulers, HasLen, 5)
	c.Assert(newOpt.LoadPDServerConfig().UseRegionStorage, IsTrue)
	for i, s := range schedulers {
		c.Assert(s.Type, Equals, defaultSchedulers[i])
		c.Assert(s.Disable, IsFalse)
	}
	c.Assert(newOpt.GetMaxReplicas("default"), Equals, 5)
	c.Assert(newOpt.GetMaxSnapshotCount(), Equals, uint64(10))
}

func (s *testConfigSuite) TestValidation(c *C) {
	cfg := NewConfig()
	c.Assert(cfg.Adjust(nil), IsNil)

	cfg.Log.File.Filename = path.Join(cfg.DataDir, "test")
	c.Assert(cfg.Validate(), NotNil)

	// check schedule config
	cfg.Schedule.HighSpaceRatio = -0.1
	c.Assert(cfg.Schedule.Validate(), NotNil)
	cfg.Schedule.HighSpaceRatio = 0.6
	c.Assert(cfg.Schedule.Validate(), IsNil)
	cfg.Schedule.LowSpaceRatio = 1.1
	c.Assert(cfg.Schedule.Validate(), NotNil)
	cfg.Schedule.LowSpaceRatio = 0.4
	c.Assert(cfg.Schedule.Validate(), NotNil)
	cfg.Schedule.LowSpaceRatio = 0.8
	c.Assert(cfg.Schedule.Validate(), IsNil)
	cfg.Schedule.TolerantSizeRatio = -0.6
	c.Assert(cfg.Schedule.Validate(), NotNil)
}

func (s *testConfigSuite) TestAdjust(c *C) {
	cfgData := `
name = ""
lease = 0

[schedule]
max-merge-region-size = 0
enable-one-way-merge = true
leader-schedule-limit = 0
`
	cfg := NewConfig()
	meta, err := toml.Decode(cfgData, &cfg)
	c.Assert(err, IsNil)
	err = cfg.Adjust(&meta)
	c.Assert(err, IsNil)

	// When invalid, use default values.
	host, err := os.Hostname()
	c.Assert(err, IsNil)
	c.Assert(cfg.Name, Equals, fmt.Sprintf("%s-%s", defaultName, host))
	c.Assert(cfg.LeaderLease, Equals, defaultLeaderLease)
	// When defined, use values from config file.
	c.Assert(cfg.Schedule.MaxMergeRegionSize, Equals, uint64(0))
	c.Assert(cfg.Schedule.EnableOneWayMerge, Equals, true)
	c.Assert(cfg.Schedule.LeaderScheduleLimit, Equals, uint64(0))
	// When undefined, use default values.
	c.Assert(cfg.PreVote, IsTrue)
	c.Assert(cfg.Schedule.MaxMergeRegionKeys, Equals, uint64(defaultMaxMergeRegionKeys))

	// Check undefined config fields
	cfgData = `
type = "pd"
name = ""
lease = 0

[schedule]
type = "random-merge"
`
	cfg = NewConfig()
	meta, err = toml.Decode(cfgData, &cfg)
	c.Assert(err, IsNil)
	err = cfg.Adjust(&meta)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(cfg.WarningMsgs[0], "Config contains undefined item"), IsTrue)

	// Check misspelled schedulers name
	cfgData = `
name = ""
lease = 0

[[schedule.schedulers]]
type = "random-merge-schedulers"
`
	cfg = NewConfig()
	meta, err = toml.Decode(cfgData, &cfg)
	c.Assert(err, IsNil)
	err = cfg.Adjust(&meta)
	c.Assert(err, NotNil)

	// Check correct schedulers name
	cfgData = `
name = ""
lease = 0

[[schedule.schedulers]]
type = "random-merge"
`
	cfg = NewConfig()
	meta, err = toml.Decode(cfgData, &cfg)
	c.Assert(err, IsNil)
	err = cfg.Adjust(&meta)
	c.Assert(err, IsNil)

	cfgData = `
[metric]
interval = "35s"
address = "localhost:9090"
`
	cfg = NewConfig()
	meta, err = toml.Decode(cfgData, &cfg)
	c.Assert(err, IsNil)
	err = cfg.Adjust(&meta)
	c.Assert(err, IsNil)

	c.Assert(cfg.Metric.PushInterval.Duration, Equals, 35*time.Second)
	c.Assert(cfg.Metric.PushAddress, Equals, "localhost:9090")
}

func newTestScheduleOption() (*ScheduleOption, error) {
	cfg := NewConfig()
	if err := cfg.Adjust(nil); err != nil {
		return nil, err
	}
	opt := NewScheduleOption(cfg)
	return opt, nil
}

func (s *testConfigSuite) TestDefaultStoreLimitUpgrade(c *C) {
	var cfgData = `
	[schedule]
	store-balance-rate = 128.0
	`

	conf := NewConfig()
	meta, err := toml.Decode(cfgData, &conf)
	c.Assert(meta, NotNil)
	c.Assert(err, IsNil)

	// After decoding, default-store-limit has not been set
	c.Assert(conf.Schedule.DefaultStoreLimit, Equals, 0.0)
	c.Assert(conf.Schedule.StoreBalanceRate, Equals, 128.0)

	// After adjusting, default-store-limit has the save value of store-blance-rate
	c.Assert(conf.Adjust(&meta), IsNil)
	c.Assert(conf.Schedule.DefaultStoreLimit, Equals, 128.0)
	c.Assert(conf.Schedule.StoreBalanceRate, Equals, 0.0)
}

func (s *testConfigSuite) TestDefaultStoreLimitOverwrite(c *C) {
	var cfgData = `
	[schedule]
	store-balance-rate = 128.0
	default-store-limit= 256.0
	`

	conf := NewConfig()
	meta, err := toml.Decode(cfgData, &conf)
	c.Assert(meta, NotNil)
	c.Assert(err, IsNil)

	// The options are decoded as usual
	c.Assert(conf.Schedule.DefaultStoreLimit, Equals, 256.0)
	c.Assert(conf.Schedule.StoreBalanceRate, Equals, 128.0)

	// After adjusting, default-store-limit overwrites store-balance-rate
	c.Assert(conf.Adjust(&meta), IsNil)
	c.Assert(conf.Schedule.DefaultStoreLimit, Equals, 256.0)
	c.Assert(conf.Schedule.StoreBalanceRate, Equals, 0.0)
}

func (s *testConfigSuite) TestDefaultStoreLimitPersist(c *C) {
	opt, err := newTestScheduleOption()
	c.Assert(err, IsNil)
	storage := core.NewStorage(kv.NewMemoryKV())
	scheduleCfg := opt.Load()
	scheduleCfg.StoreBalanceRate = 10.0
	scheduleCfg.DefaultStoreLimit = 0.0
	c.Assert(opt.Persist(storage), IsNil)

	newOpt, err := newTestScheduleOption()
	c.Assert(err, IsNil)
	c.Assert(newOpt.Reload(storage), IsNil)
	scheduleCfg = newOpt.Load()
	c.Assert(scheduleCfg.DefaultStoreLimit, Equals, 10.0)
	c.Assert(scheduleCfg.StoreBalanceRate, Equals, 0.0)
}
