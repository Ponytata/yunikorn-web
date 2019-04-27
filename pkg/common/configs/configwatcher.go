/*
Copyright 2019 The Unity Scheduler Authors

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

package configs

import (
	"bytes"
	"github.com/golang/glog"
	"sync"
	"time"
)

var configWatcher *ConfigWatcher
var once sync.Once

// Config watcher watches on a configuration file,
// it is initiated with a certain expiration time, it will stop running either
// it detects configuration changes, or the expiration time reaches
type ConfigWatcher struct {
	rmId        string
	policyGroup string
 	reloader    ConfigReloader
	expireTime  time.Duration
	running     bool
	lock        *sync.Mutex
}

// interface for the actual reload function
type ConfigReloader interface {
	DoReloadConfiguration() error
}

func CreateConfigWatcher(rmId string, policyGroup string, expiration time.Duration) *ConfigWatcher {
	return &ConfigWatcher{
		rmId:        rmId,
		policyGroup: policyGroup,
		expireTime:  expiration,
		lock:        &sync.Mutex{},
	}
}

func GetInstance() *ConfigWatcher {
	// singleton
	once.Do(func() {
		configWatcher = &ConfigWatcher{
			expireTime:      60 * time.Second,
			lock:            &sync.Mutex{},
		}
	})

	return configWatcher
}

func (cw *ConfigWatcher) RegisterCallback(reloader ConfigReloader) {
	cw.lock.Lock()
	defer cw.lock.Unlock()
	cw.reloader = reloader
}

// returns true if config file state remains same,
// returns false if config file state changes
func (cw *ConfigWatcher) runOnce() bool {
	cw.lock.Lock()
	defer cw.lock.Unlock()

	newConfig, err := SchedulerConfigLoader(cw.policyGroup)
	if err != nil {
		glog.V(1).Infof("failed to calculate the checksum of" +
			" configuration file for policyGroup %s, ignore reloading configuration",
			cw.policyGroup)
		return false
	}

	// acquire the lock to avoid Checksum changed externally
	same := bytes.Equal(newConfig.Checksum, ConfigContext.Get(cw.policyGroup).Checksum)
	if same {
		// check sum equals, file not changed
		glog.V(1).Infof("configuration file state is not unchanged")
		time.Sleep(1 * time.Second)
		return true
	} else {
		// when detect state changes, trigger the reload function
		glog.V(3).Infof("configuration file state changes")
		if err := cw.reloader.DoReloadConfiguration(); err == nil {
			glog.V(3).Infof("configuration is successfully reloaded")
		}
		return false
	}
}

// if configWatcher is not running, kick-off running it
// if configWatcher is already running, this is a noop
func (cw *ConfigWatcher) Run() {
	if !cw.running {
		cw.running = true
		ticker := time.NewTicker(1 * time.Second)
		quit := make(chan bool)
		go func() {
			for {
				select {
				case <- ticker.C:
					if !cw.runOnce() {
						cw.running = false
						return
					}
				case <- quit:
					cw.running = false
					ticker.Stop()
					return
				}
			}
		}()

		time.AfterFunc(cw.expireTime, func() {
			quit <- true
		})
	} else {
		glog.V(3).Infof("config watcher is already running")
	}
}