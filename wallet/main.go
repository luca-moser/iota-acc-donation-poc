package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Mandala/go-log"
	"github.com/beevik/ntp"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/event"
	"github.com/iotaledger/iota.go/account/event/listener"
	"github.com/iotaledger/iota.go/account/plugins/promoter"
	"github.com/iotaledger/iota.go/account/plugins/transfer/poller"
	"github.com/iotaledger/iota.go/account/store"
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

type NTPClock struct {
	server string
}

func (clock *NTPClock) Now() (time.Time, error) {
	t, err := ntp.Time(clock.server)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "NTP clock error")
	}
	return t.UTC(), nil
}

type config struct {
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

func readConfig() *config {
	configBytes, err := ioutil.ReadFile(configFile)
	must(err)

	config := &config{}
	must(json.Unmarshal(configBytes, config))
	return config
}

func handleShutdown() {

}

func main() {
	var acc account.Account
	var badger *store.BadgerStore

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
		if badger != nil {
			if err := badger.Close(); err != nil {
				logger.Fatal("couldn't close store gracefully")
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

	// make sure data dir exists
	os.MkdirAll(conf.DataDir, os.ModePerm)

	// init store for the account
	badger, err = store.NewBadgerStore(conf.DataDir)
	must(err)

	// init NTP time source
	ntpClock := &NTPClock{conf.Time.NTPServer}

	// init account
	em := event.NewEventMachine()

	// create a poller which will check for incoming transfers
	receiveEventFilter := poller.NewPerTailReceiveEventFilter(true)
	transferPoller := poller.NewTransferPoller(
		iotaAPI, badger, em, account.NewInMemorySeedProvider(conf.Seed), receiveEventFilter,
		time.Duration(conf.TransferPollInterval)*time.Second,
	)

	// create a promoter/reattacher which takes care of trying to get
	// pending transfers to confirm
	promoterReattacher := promoter.NewPromoter(
		iotaAPI, badger, em, ntpClock,
		time.Duration(conf.PromoteReattachInterval)*time.Second,
		conf.GTTADepth, conf.MWM)

	// build the account object
	acc, err = account.New(iotaAPI, badger).
		Seed(conf.Seed).Clock(ntpClock).
		SecurityLevel(consts.SecurityLevel(conf.SecurityLevel)).
		MWM(conf.MWM).Depth(conf.GTTADepth).
		With(transferPoller, promoterReattacher).
		WithEvents(em).
		Build()
	must(err)
	must(acc.Start())

	// generate an own deposit address which expires in 3 days
	// (just for having an address to send funds to for initial funding)
	timeQueryS := time.Now()
	logger.Infof("querying time via NTP server %s", conf.Time.NTPServer)
	now, err := ntpClock.Now()
	must(err)
	logger.Infof("took %v to query time from %s", time.Now().Sub(timeQueryS), conf.Time.NTPServer)

	now = now.Add(time.Duration(2) * time.Hour)
	logger.Infof("generating fresh deposit address with validity until %s....\n", now.Format(dateFormat))
	depCond, err := acc.AllocateDepositRequest(&deposit.Request{TimeoutAt: &now})
	must(err)
	logger.Info("own address: ", depCond.Address)

	// log all events happening around the account
	logAccountEvents(em, acc)

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
			printState(badger, conf.Seed)
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

		// get the current time from the NTP server
		currentTime, err := ntpClock.Now()
		must(err)

		// check whether the magnet link expired
		if currentTime.After(*conds.TimeoutAt) {
			logger.Errorf("the magnet-link is expired on %s, (it's currently %s)\n", conds.TimeoutAt.Format(dateFormat), currentTime.Format(dateFormat))
			continue
		}

		// check whether we are still in time for doing this transfer
		if currentTime.Add(time.Duration(24) * time.Hour).After(*conds.TimeoutAt) {
			logger.Error("the magnet link expires in less than 24 hours, please request a new one")
			continue
		}

		// send the transfer
		logger.Info("sending", 10, "iotas to", conds.Address)
		recipient := account.Recipient{Address: conds.Address, Value: 10}
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

func printState(store store.Store, seed string) {
	state, err := store.LoadAccount(fmt.Sprintf("%x", sha256.Sum256([]byte(seed))))
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

func logAccountEvents(em event.EventMachine, acc account.Account) {
	// create a new listener which listens on the given account events
	lis := listener.NewEventListener(em).All()

	go func() {
		defer lis.Close()
	exit:
		for {
			select {
			case ev := <-lis.Promotion:
				logger.Infof("(event) promoted %s with %s\n", ev.BundleHash[:10], ev.PromotionTailTxHash)
			case ev := <-lis.Reattachment:
				logger.Infof("(event) reattached %s with %s\n", ev.BundleHash[:10], ev.ReattachmentTailTxHash)
			case ev := <-lis.Sending:
				tail := ev[0]
				logger.Infof("(event) sending %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.Sent:
				tail := ev[0]
				logger.Infof("(event) send (confirmed) %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivingDeposit:
				tail := ev[0]
				logger.Infof("(event) receiving deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-lis.ReceivedDeposit:
				tail := ev[0]
				logger.Infof("(event) received deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
				printBalance(acc)
			case ev := <-lis.ReceivedMessage:
				tail := ev[0]
				logger.Infof("(event) received msg %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case err := <-lis.InternalError:
				logger.Errorf("received internal error: %s\n", err.Error())
			case <-lis.Shutdown:
				logger.Info("account got gracefully shutdown")
				break exit
			}
		}
	}()
}
