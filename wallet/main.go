package main

import (
	"encoding/json"
	"fmt"
	"github.com/Mandala/go-log"
	"github.com/beevik/ntp"
	"github.com/iotaledger/iota.go/account"
	"github.com/iotaledger/iota.go/account/deposit"
	"github.com/iotaledger/iota.go/account/store"
	"github.com/iotaledger/iota.go/api"
	"github.com/iotaledger/iota.go/consts"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/http"
	"os"
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

func main() {
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
	badger, err := store.NewBadgerStore(conf.DataDir)
	must(err)

	// init NTP time source
	ntpClock := &NTPClock{conf.Time.NTPServer}

	// init account
	eventMachine := account.NewEventMachine()
	acc, err := account.NewBuilder(iotaAPI, badger).Seed(conf.Seed).
		SecurityLevel(consts.SecurityLevel(conf.SecurityLevel)).
		MWM(conf.MWM).Depth(conf.GTTADepth).Clock(ntpClock).
		PromoteReattachInterval(conf.PromoteReattachInterval).
		TransferPollInterval(conf.TransferPollInterval).
		ReceiveEventFilter(account.NewPerTailReceiveEventFilter(true)).
		WithEvents(eventMachine).
		Build()
	must(err)
	must(acc.Start())
	defer acc.Shutdown()

	// generate an own deposit address which expires in 3 days
	// (just for having an address to send funds to for initial funding)
	timeQueryS := time.Now()
	logger.Infof("querying time via NTP server %s", conf.Time.NTPServer)
	now, err := ntpClock.Now()
	must(err)
	logger.Infof("took %v to query time from %s", time.Now().Sub(timeQueryS), conf.Time.NTPServer)

	now = now.AddDate(0, 0, 3)
	logger.Infof("generating fresh deposit address with validity until %s....\n", now.Format(dateFormat))
	depCond, err := acc.NewDepositRequest(&deposit.Request{TimeoutOn: &now})
	must(err)
	logger.Info("own address: ", depCond.Address)

	// log all events happening around the account
	logAccountEvents(eventMachine, acc)

	// wait for magnet-link input
	for {
		printBalance(acc)

		logger.Info("Enter magnet-link:")
		var rawLink string
		if _, err := fmt.Scanln(&rawLink); err != nil {
			continue
		}

		// parse the magnet link
		conds, err := deposit.ParseMagnetLink(rawLink)
		if err != nil {
			logger.Error("invalid magnet link supplied:", err.Error())
			continue
		}

		// get the current time from the NTP server
		currentTime, err := ntpClock.Now()
		must(err)

		// check whether the magnet link expired
		if currentTime.After(*conds.TimeoutOn) {
			logger.Errorf("the magnet-link is expired on %s, (it's currently %s)\n", conds.TimeoutOn.Format(dateFormat), currentTime.Format(dateFormat))
			continue
		}

		// check whether we are still in time for doing this transfer
		if currentTime.Add(time.Duration(24) * time.Hour).After(*conds.TimeoutOn) {
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

func printBalance(acc account.Account) {
	balance, err := acc.UsableBalance()
	must(err)
	logger.Info("current balance", balance, "iotas")
}

func logAccountEvents(em account.EventMachine, acc account.Account) {

	// create a new listener which listens on the given account events
	listener := account.NewEventListener(em).All()

	go func() {
	exit:
		for {
			select {
			case ev := <-listener.Promotion:
				logger.Infof("(event) promoted %s with %s\n", ev.BundleHash[:10], ev.PromotionTailTxHash)
			case ev := <-listener.Reattachment:
				logger.Infof("(event) reattached %s with %s\n", ev.BundleHash[:10], ev.ReattachmentTailTxHash)
			case ev := <-listener.Sending:
				tail := ev[0]
				logger.Infof("(event) sending %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.Sent:
				tail := ev[0]
				logger.Infof("(event) send (confirmed) %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.ReceivingDeposit:
				tail := ev[0]
				logger.Infof("(event) receiving deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case ev := <-listener.ReceivedDeposit:
				tail := ev[0]
				logger.Infof("(event) received deposit %s with tail %s\n", tail.Bundle[:10], tail.Hash)
				printBalance(acc)
			case ev := <-listener.ReceivedMessage:
				tail := ev[0]
				logger.Infof("(event) received msg %s with tail %s\n", tail.Bundle[:10], tail.Hash)
			case errorEvent := <-em.InternalAccountErrors():
				logger.Errorf("received internal error: %s\n", errorEvent.Error)
			case <-listener.Shutdown:
				logger.Info("account got gracefully shutdown")
				break exit
			}
		}
	}()
}
