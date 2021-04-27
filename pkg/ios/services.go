package ios

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/scionproto/scion/go/cs/config"
	dispatcher "github.com/scionproto/scion/go/dispatcher"
	libconfig "github.com/scionproto/scion/go/lib/config"
	"github.com/scionproto/scion/go/lib/infra/modules/itopo"
	"github.com/scionproto/scion/go/lib/log"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/pkg/app/launcher"
	"github.com/scionproto/scion/go/pkg/service"
	sciond "github.com/scionproto/scion/go/sciond"
	"github.com/spf13/viper"
)

var globalCfg config.Config

func RunScion() {
	application := launcher.Application{
		TOMLConfig: &globalCfg,
		ShortName:  "SCION Dispatcher/Daemon",
		Main:       realMain,
	}
	application.Run()
}

var ConfigPath string;

func SetDaemonConfigPath(path string) {
		// Load launcher configurations from the same config file as the custom
	// application configuration.
	config := viper.New()
	config.SetConfigType("toml")
	config.SetConfigFile(path)
	if err := config.ReadInConfig(); err != nil {
		fmt.Println(serrors.WrapStr("loading generic server config from file", err,
			"file", path))
	}

	if err := libconfig.LoadFile(path, sciond.Daemon_Config()); err != nil {
		fmt.Println(serrors.WrapStr("loading config from file", err,
			"file", path))
	}
	sciond.Daemon_Config().InitDefaults()
	// 
	// a.config = viper.New()
	// a.config.SetDefault(cfgLogConsoleLevel, log.DefaultConsoleLevel)
	// a.config.SetDefault(cfgLogConsoleFormat, "human")
	// a.config.SetDefault(cfgLogConsoleStacktraceLevel, log.DefaultStacktraceLevel)
	// a.config.SetDefault(cfgGeneralID, executable)
	// // The configuration file location is specified through command-line flags.
	// // Once the comand-line flags are parsed, we register the location of the
	// // config file with the viper config.
	// a.config.SetDefault(cfgConfigFile, ConfigPath)// BindPFlag(cfgConfigFile, cmd.Flags().Lookup(cfgConfigFile))

}

func SetDispatcherConfigPath(path string) {
		// Load launcher configurations from the same config file as the custom
	// application configuration.
	config := viper.New()
	config.SetConfigType("toml")
	config.SetConfigFile(path)
	if err := config.ReadInConfig(); err != nil {
		fmt.Println(serrors.WrapStr("loading generic server config from file", err,
			"file", path))
	}

	if err := libconfig.LoadFile(path, dispatcher.Dispatcher_Config()); err != nil {
		fmt.Println(serrors.WrapStr("loading config from file", err,
			"file", path))
	}
	dispatcher.Dispatcher_Config().InitDefaults()

	// 
	// a.config = viper.New()
	// a.config.SetDefault(cfgLogConsoleLevel, log.DefaultConsoleLevel)
	// a.config.SetDefault(cfgLogConsoleFormat, "human")
	// a.config.SetDefault(cfgLogConsoleStacktraceLevel, log.DefaultStacktraceLevel)
	// a.config.SetDefault(cfgGeneralID, executable)
	// // The configuration file location is specified through command-line flags.
	// // Once the comand-line flags are parsed, we register the location of the
	// // config file with the viper config.
	// a.config.SetDefault(cfgConfigFile, ConfigPath)// BindPFlag(cfgConfigFile, cmd.Flags().Lookup(cfgConfigFile))

}

func runDispatcher() {
	err := dispatcher.Dispatcher_RealMain()
	fmt.Printf("ERROR: Dispatcher terminated: %s\n", err)
}

func runSciond() {
	err := sciond.Sciond_RealMain()
	fmt.Printf("ERROR: Sciond terminated: %s\n", err)
}

func realMain() error {
	statusPages := service.StatusPages{
		"info":      service.NewInfoHandler(),
		"daemon_config":    service.NewConfigHandler(sciond.Daemon_Config()),
		"dispatcher_config":    service.NewConfigHandler(dispatcher.Dispatcher_Config()),
		"topology":  itopo.TopologyHandler,
		"log/level": log.ConsoleLevel.ServeHTTP,
	}
	
	if err := statusPages.Register(http.DefaultServeMux, globalCfg.General.ID); err != nil {
		return serrors.WrapStr("registering status pages", err)
	}

	fmt.Println("Starting dispatcher")
	go runDispatcher()
	
	time.Sleep(1)

	fmt.Println("Starting daemon")
	runSciond()

	return nil
}

func SetDispatcherSocket(path string) error {
	return os.Setenv("SCION_DISPATCHER_SOCKET", path)
}

func SetSciondAddress(addr string) error {
	return os.Setenv("SCION_DAEMON_ADDRESS", addr)
}
