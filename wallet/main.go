package main

import (
	"encoding/json"
	"fmt"
	"github.com/Mandala/go-log"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/builder"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/event"
	"github.com/iotaledger/iota.go/account/oracle"
	oracle_time "github.com/iotaledger/iota.go/account/oracle/time"
	"github.com/iotaledger/iota.go/account/plugins/promoter"
	"github.com/iotaledger/iota.go/account/plugins/transfer/poller"
	"github.com/iotaledger/iota.go/account/store"
	mongo_store "github.com/iotaledger/iota.go/account/store/mongo"
	"github.com/iotaledger/iota.go/account/timesrc"
	"github.com/iotaledger/iota.go/api"
	"github.com/iotaledger/iota.go/consts"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const configFile = "wallet.json"
const dateFormat = "2006-02-01 15:04:05"

var logger *log.Logger

func main() {
	var acc account.Account
	var dataStore *mongo_store.MongoStore

	// shutdown function called on interrupts or panics
	defer func() {
		if r := recover(); r != nil {
			logger.Fatal("program panicked:", r)
		}
		if acc != nil {
			if err := acc.Shutdown(); err != nil {
				logger.Fatal("couldn't shutdown account gracefully")
			}
		}
		logger.Info("bye!")
	}()

	logger = log.New(os.Stdout)
	conf := readConfig()

	// compose quorum API
	quorumConf := conf.Quorum
	httpClient := &http.Client{Timeout: time.Duration(quorumConf.Timeout) * time.Second}
	iotaAPI, err := api.ComposeAPI(api.QuorumHTTPClientSettings{
		PrimaryNode:                &quorumConf.PrimaryNode,
		Threshold:                  quorumConf.Threshold,
		NoResponseTolerance:        quorumConf.NoResponseTolerance,
		Client:                     httpClient,
		Nodes:                      quorumConf.Nodes,
		MaxSubtangleMilestoneDelta: quorumConf.MaxSubtangleMilestoneDelta,
	}, api.NewQuorumHTTPClient)
	must(err)

	// init store for the account
	mongoConf := conf.MongoDB
	dataStore, err = mongo_store.NewMongoStore(mongoConf.URI, &mongo_store.Config{
		DBName: mongoConf.DBName, CollName: mongoConf.CollName,
	})
	must(err)

	// init NTP time source
	ntpClock := timesrc.NewNTPTimeSource(conf.Time.NTPServer)

	// init account
	em := event.NewEventMachine()

	// build the account object
	b := builder.NewBuilder().
		WithAPI(iotaAPI).
		WithStore(dataStore).
		WithSeed(conf.Seed).
		WithTimeSource(ntpClock).
		WithSecurityLevel(consts.SecurityLevel(conf.SecurityLevel)).
		WithMWM(conf.MWM).
		WithDepth(conf.GTTADepth).
		WithEvents(em)

	// create a poller which will check for incoming transfers
	transferPoller := poller.NewTransferPoller(
		b.Settings(),
		poller.NewPerTailReceiveEventFilter(true),
		time.Duration(conf.TransferPollInterval)*time.Second,
	)

	// create a promoter/reattacher which takes care of trying to get
	// pending transfers to confirm.
	promoterReattacher := promoter.NewPromoter(b.Settings(), time.Duration(conf.PromoteReattachInterval)*time.Second)

	acc, err = b.Build(transferPoller, promoterReattacher, NewLogPlugin(em))
	must(err)
	must(acc.Start())

	// test time source
	timeQueryS := time.Now()
	logger.Infof("querying time via NTP server %s", conf.Time.NTPServer)
	now, err := ntpClock.Time()
	must(err)
	logger.Infof("took %v to query time from %s", time.Now().Sub(timeQueryS), conf.Time.NTPServer)

	// generate a deposit address which expires in 2 hours
	now = now.Add(time.Duration(2) * time.Hour)
	logger.Infof("generating fresh deposit address with validity until %s....\n", now.Format(dateFormat))
	depCond, err := acc.AllocateDepositRequest(&deposit.Request{TimeoutAt: &now})
	must(err)
	logger.Info("own address: ", depCond.Address)

	// create an oracle which helps us to decide whether we should send a transaction.
	// we only send a transaction if the timeout is more than 5 hours away.
	sendOracle := oracle.New(oracle_time.NewTimeDecider(ntpClock, time.Duration(5)*time.Hour))

	// listen for interrupt signals
	interruptChan := make(chan os.Signal, 2)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// read in stdin input
	commandChan := make(chan string)
	go func() {
		for {
			var rawLink string
			if _, err := fmt.Scanln(&rawLink); err != nil {
				continue
			}
			commandChan <- rawLink
		}
	}()

exit:
	for {
		printBalance(acc)
		logger.Info("Enter magnet-link:")

		// read in next signal
		var command string
		select {
		case <-interruptChan:
			logger.Info("shutting down wallet...")
			break exit
		case cmd := <-commandChan:
			command = cmd
		}

		if command == "state" {
			printState(dataStore, acc.ID())
			continue
		}

		if command == "balance" {
			continue
		}

		// parse the magnet link
		conds, err := deposit.ParseMagnetLink(command)
		if err != nil {
			logger.Error("invalid magnet link supplied:", err.Error())
			continue
		}

		ok, info, err := sendOracle.OkToSend(conds)
		if err != nil {
			logger.Error("send oracle returned an error:", err.Error())
			continue
		}
		if !ok {
			logger.Error("won't send transaction:", info)
			continue
		}

		// send the transfer
		logger.Info("sending", 10, "iotas to", conds.Address)
		recipient := conds.AsTransfer()
		recipient.Value = 10
		_, err = acc.Send(recipient)
		switch errors.Cause(err) {
		case consts.ErrInsufficientBalance:
			logger.Error("insufficient funds for doing the transfer (!)")
			continue
		case nil:
		default:
			logger.Error("got error from send operation:", err.Error())
		}
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func printState(store store.Store, id string) {
	state, err := store.LoadAccount(id)
	stateJson, err := json.MarshalIndent(state, "", "   ")
	must(err)
	fmt.Print(string(stateJson))
	fmt.Println()
}

func printBalance(acc account.Account) {
	logger.Info("querying balance...")
	s := time.Now()
	balance, err := acc.AvailableBalance()
	if err != nil {
		logger.Infof("unable to fetch balance %s", err.Error())
		return
	}
	logger.Infof("current balance %d iotas (took %v)", balance, time.Now().Sub(s))
}

type config struct {
	Seed   string `json:"seed"`
	Quorum struct {
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
	MongoDB struct {
		URI      string `json:"uri"`
		DBName   string `json:"dbname"`
		CollName string `json:"collname"`
	} `json:"mongodb"`
}

func readConfig() *config {
	configBytes, err := ioutil.ReadFile(configFile)
	must(err)

	config := &config{}
	must(json.Unmarshal(configBytes, config))
	return config
}
