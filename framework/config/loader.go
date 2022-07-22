package config

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"github.com/ghodss/yaml"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type (
	// LoadConfig provides configuration for the loader
	LoadConfig struct {
		legacy           bool
		logLegacy        bool
		additionalConfig []string
		basedir          string
		debug            bool
		cueDebugPath     []string
		cueDebugCallback func([]byte, error)
	}

	// LoadOption to be passed to Load(, ...)
	LoadOption func(*LoadConfig)
)

// DebugLog enables/disabled detailed debug logging
func DebugLog(debug bool) LoadOption {
	return func(config *LoadConfig) {
		config.debug = debug
	}
}

// CueDebug enables a cue.Instance debugger. This is part of a dev-api and might change!
func CueDebug(path []string, callback func([]byte, error)) LoadOption {
	return func(config *LoadConfig) {
		config.cueDebugPath = path
		config.cueDebugCallback = callback
	}
}

// LegacyMapping controls if flamingo legacy config mapping happens
func LegacyMapping(mapLegacy, logLegacy bool) LoadOption {
	return func(config *LoadConfig) {
		config.legacy = mapLegacy
		config.logLegacy = logLegacy
	}
}

// AdditionalConfig adds additional config values (yaml strings) to the config
func AdditionalConfig(addtionalConfig []string) LoadOption {
	return func(config *LoadConfig) {
		config.additionalConfig = append(config.additionalConfig, addtionalConfig...)
	}
}

// Load configuration in basedir
func Load(root *Area, basedir string, options ...LoadOption) error {
	config := &LoadConfig{
		legacy:  true,
		basedir: basedir,
	}
	for _, option := range options {
		option(config)
	}
	if err := loadConfigFromBasedir(root, config); err != nil {
		return err
	}
	if config.cueDebugCallback != nil {
		if err := root.loadConfig(false, false); err != nil {
			log.Println(err)
		}
		config.cueDebugCallback(format.Node(root.cueInstance.Lookup(config.cueDebugPath...).Syntax(), format.Simplify()))
	}
	return root.loadConfig(config.legacy, config.logLegacy)
}

func loadConfigFromBasedir(root *Area, config *LoadConfig) error {
	if err := load(root, config.basedir, "/", config); err != nil {
		return err
	}

	// load additional single context file
	for _, file := range strings.Split(os.Getenv("CONTEXTFILE"), ":") {
		file = strings.TrimSuffix(file, filepath.Ext(file))
		if file == "" {
			continue
		}
		loadLogged(root, loadYamlFile, file, config.debug)
		loadLogged(root, loadCueFile, file, config.debug)
	}

	for _, add := range config.additionalConfig {
		if config.debug {
			log.Printf("Loading %q", add)
		}
		if err := loadYamlConfig(root, []byte(add)); err != nil {
			return err
		}
	}

	return nil
}

// LoadConfigFile loads a config
// Deprecated: do not arbitrarily load anything anymore, use Area.Load
func LoadConfigFile(area *Area, file string) error {
	log.Println("WARNING! config.LoadConfigFile is deprecated!")

	if err := loadYamlFile(area, file); err != nil {
		return err
	}
	if err := loadCueFile(area, file); err != nil {
		return err
	}
	return nil
}

func loadLogged(area *Area, loader func(*Area, string) error, filename string, debug bool) {
	if debug {
		log.Printf("Loading %q", filename)
	}
	if err := loader(area, filename); err != nil && debug {
		log.Printf("Error: %s", err)
	}
}

func load(area *Area, basedir, curdir string, config *LoadConfig) error {
	loadLogged(area, loadYamlFile, filepath.Join(basedir, curdir, "config"), config.debug)
	loadLogged(area, loadCueFile, filepath.Join(basedir, curdir, "config"), config.debug)
	loadLogged(area, loadYamlRoutesFile, filepath.Join(basedir, curdir, "routes"), config.debug)
	for _, context := range strings.Split(os.Getenv("CONTEXT"), ":") {
		if context == "" {
			continue
		}
		loadLogged(area, loadYamlFile, filepath.Join(basedir, curdir, "config_"+context+""), config.debug)
		loadLogged(area, loadCueFile, filepath.Join(basedir, curdir, "config_"+context+""), config.debug)
		loadLogged(area, loadYamlRoutesFile, filepath.Join(basedir, curdir, "routes_"+context+""), config.debug)
	}
	loadLogged(area, loadYamlFile, filepath.Join(basedir, curdir, "config_local"), config.debug)
	loadLogged(area, loadCueFile, filepath.Join(basedir, curdir, "config_local"), config.debug)
	loadLogged(area, loadYamlRoutesFile, filepath.Join(basedir, curdir, "routes_local"), config.debug)

	for _, child := range area.Childs {
		if err := load(child, basedir, filepath.Join(curdir, child.Name), config); err != nil {
			return err
		}
	}
	return nil
}

func loadCueFile(area *Area, filename string) error {
	f, err := os.Open(filename + ".cue")
	if f != nil {
		_ = f.Close()
	}
	if err != nil {
		return nil
	}

	file, err := parser.ParseFile(filename+".cue", nil)
	if err != nil {
		return err
	}
	area.cueConfig = cueAstMergeFile(area.cueConfig, file)

	return nil
}

var envRegex = regexp.MustCompile(`%%ENV:([^%\n]+)%%(([^%\n]+)%%)?`)
var etcdRegex = regexp.MustCompile(`%%ETCD:([^%\n]+)%%(([^%\n]+)%%)?`)

func loadYamlFile(area *Area, filename string) error {
	config, err := ioutil.ReadFile(filename + ".yml")
	if err == nil {
		return loadYamlConfig(area, config)
	}

	config, err = ioutil.ReadFile(filename + ".yaml")
	if err == nil {
		return loadYamlConfig(area, config)
	}

	return fmt.Errorf("can not load %s.yml nor %s.yaml", filename, filename)
}

func getEtcdClient(config []byte) (*clientv3.Client, time.Duration, bool) {
	cfg := make(Map)
	if err := yaml.Unmarshal(config, &cfg); err != nil {
		panic(err)
	}

	temp := make(Map)
	if err := temp.Add(cfg); err != nil {
		panic(err)
	}

	subCfg, ok := cfg.Get("flamingo")
	if !ok {
		return nil, 0, false
	}

	subCfg, ok = subCfg.(map[string]interface{})["etcd"]
	if !ok {
		return nil, 0, false
	}

	host, _ := subCfg.(map[string]interface{})["host"]
	username, ok := subCfg.(map[string]interface{})["username"]
	if !ok {
		username = ""
	}
	password, ok := subCfg.(map[string]interface{})["password"]
	if !ok {
		password = ""
	}

	cli, err := clientv3.New(clientv3.Config{
		DialTimeout: 20 * time.Second,
		Endpoints:   []string{fmt.Sprint(host)},
		Username:    fmt.Sprint(username),
		Password:    fmt.Sprint(password),
	})
	if err != nil {
		panic(err)
	}

	return cli, 10 * time.Second, true
}

func loadYamlConfig(area *Area, config []byte) error {
	config = envRegex.ReplaceAllFunc(
		config,
		func(a []byte) []byte {
			value := os.Getenv(string(envRegex.FindSubmatch(a)[1]))
			if value == "" {
				value = string(envRegex.FindSubmatch(a)[3])
			}
			return []byte(value)
		},
	)

	cli, requestTimeout, ok := getEtcdClient(config)
	if ok {
		defer cli.Close()
		config = etcdRegex.ReplaceAllFunc(
			config,
			func(a []byte) []byte {
				submatch := etcdRegex.FindSubmatch(a)
				key := string(submatch[1])
				ctx, canFn := context.WithTimeout(context.Background(), requestTimeout)
				defer canFn()
				response, err := cli.Get(ctx, key)
				if err != nil {
					panic(err)
				}

				var value string
				if len(response.Kvs) > 0 {
					value = string(response.Kvs[0].Value)
				} else if len(submatch) > 3 {
					value = string(submatch[3])
				}
				return []byte(value)
			},
		)
	}

	cfg := make(Map)
	if err := yaml.Unmarshal(config, &cfg); err != nil {
		panic(err)
	}

	if area.loadedConfig == nil {
		area.loadedConfig = make(Map)
	}

	return area.loadedConfig.Add(cfg)
}

func loadYamlRoutesFile(area *Area, filename string) error {
	routes, err := ioutil.ReadFile(filename + ".yml")
	if err == nil {
		return yaml.Unmarshal(routes, &area.Routes)
	}

	routes, err = ioutil.ReadFile(filename + ".yaml")
	if err == nil {
		return yaml.Unmarshal(routes, &area.Routes)
	}

	return fmt.Errorf("can not load %s.yml nor %s.yaml", filename, filename)
}
