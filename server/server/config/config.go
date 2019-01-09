package config

import (
	"encoding/json"
	"io/ioutil"
	"reflect"
	"strings"
)

type Config interface{}

var subConfigs = []Config{&AppConfig{}}

func LoadConfig() *Configuration {
	configuration := &Configuration{}
	refConfig := reflect.Indirect(reflect.ValueOf(configuration))

	// go through each sub config, load it and init it on the main struct
	for _, c := range subConfigs {
		// indirect as 'c' is pointer to struct
		ind := reflect.Indirect(reflect.ValueOf(c))
		ty := ind.Type()
		field, _ := ty.FieldByName("Location")
		fileLocation := field.Tag.Get("loc")

		// read file indicated by the field tag
		fileBytes, err := ioutil.ReadFile(fileLocation)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(fileBytes, c); err != nil {
			panic(err)
		}

		// init configuration struct field with the given config
		configFieldName := strings.Split(ty.Name(), "Config")[0]
		refConfig.FieldByName(configFieldName).Set(ind)
	}
	return configuration
}

type Configuration struct {
	App AppConfig
}

type AppConfig struct {
	Location interface{} `loc:"./configs/app.json"`
	Name     string
	Dev      bool
	Verbose  bool
	Account  AccountConfig
	HTTP     WebConfig
}

type AccountConfig struct {
	Seed    string `json:"seed"`
	DataDir string `json:"data_dir"`
	Quorum  struct {
		PrimaryNode                string   `json:"primary_node"`
		Nodes                      []string `json:"nodes"`
		Threshold                  float64  `json:"threshold"`
		NoResponseTolerance        float64  `json:"no_response_tolerance"`
		MaxSubtangleMilestoneDelta uint64   `json:"max_subtangle_milestone_delta"`
		Timeout                    uint64   `json:"timeout"`
	} `json:"quorum"`
	MWM                        uint64 `json:"mwm"`
	GTTADepth                  uint64 `json:"gtta_depth"`
	SecurityLevel              uint64 `json:"security_level"`
	TransferPollInterval       uint64 `json:"transfer_poll_interval"`
	PromoteReattachInterval    uint64 `json:"promote_reattach_interval"`
	AddressValidityTimeoutDays uint64 `json:"address_validity_timeout_days"`
	Time                       struct {
		NTPServer string `json:"ntp_server"`
	} `json:"time"`
}

type WebConfig struct {
	Domain  string
	Address string
	Assets  struct {
		Static  string
		HTML    string
		Favicon string
	}
	LogRequests bool
}
